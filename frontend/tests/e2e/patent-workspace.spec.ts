import { expect, test, type Page, type Route } from '@playwright/test'

const timestamp = '2026-10-21T00:00:00Z'
async function setup(page: Page, enabled = true) {
  const user = { id: 'patent-user', username: 'Patent User', email: 'patent@example.test', tenant_id: 1, is_active: true, created_at: timestamp, updated_at: timestamp }
  const tenant = { id: 1, name: 'Patent Test', owner_id: user.id, created_at: timestamp, updated_at: timestamp }
  const memberships = [{ tenant_id: 1, role: 'owner' }]
  await page.addInitScript(({ user, tenant, memberships }) => {
    localStorage.setItem('weknora_token', 'patent-test-token')
    localStorage.setItem('weknora_selected_tenant_id', '1')
    localStorage.setItem('weknora_user', JSON.stringify(user))
    localStorage.setItem('weknora_tenant', JSON.stringify(tenant))
    localStorage.setItem('weknora_memberships', JSON.stringify(memberships))
    localStorage.setItem('locale', 'en-US')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
  }, { user, tenant, memberships })
  const modes = ['platform', 'legal_assistant', 'patent_search', 'patent_writing']
  const sessions = modes.map(workspace_mode => ({ id: `${workspace_mode}-session`, title: `${workspace_mode} history`, workspace_mode, created_at: timestamp, updated_at: timestamp, user_id: user.id }))
  const agents = modes.map(mode => ({ id: mode === 'platform' ? 'builtin-quick-answer' : `builtin-${mode.replace('_', '-')}`, name: mode, is_builtin: true, config: { agent_mode: 'smart-reasoning', model_id: 'chat', rerank_model_id: 'rerank', web_search_enabled: true, kb_selection_mode: 'all', allowed_tools: ['search_knowledge', 'read_document'] } }))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    const reply = (data: unknown, status = 200, extra: Record<string, unknown> = {}) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ success: status < 400, data, ...extra }) })
    if (path.endsWith('/auth/me')) return reply({ user, tenant, memberships, capabilities: { can_create_tenant: false } })
    if (path.endsWith('/system/capabilities')) return reply({ edition: 'community', capabilities: {} })
    if (path.endsWith('/patent-workspace-config')) return reply({ enabled })
    if (path.endsWith('/legal-workspace-config')) return reply({ enabled: true })
    if (path.endsWith('/web-search-providers')) return reply([{ id: 'search', name: 'Test Search', provider: 'bing', is_default: true, parameters: {} }])
    if (path.endsWith('/knowledge-bases')) return reply([{ id: 'search-kb', name: 'Patent Materials', type: 'document', knowledge_count: 1, summary_model_id: 'chat', embedding_model_id: 'embedding' }])
    if (/\/patent-(search|writing)\/sessions$/.test(path) && route.request().method() === 'POST') {
      const mode = path.includes('patent-search') ? 'patent_search' : 'patent_writing'
      const session = { ...sessions.find(s => s.workspace_mode === mode)!, id: `${mode}-new`, title: 'New conversation' }
      sessions.push(session)
      return reply(session)
    }
    if (path.endsWith('/sessions')) {
      const mode = new URL(route.request().url()).searchParams.get('workspace_mode')
      const rows = sessions.filter(s => !mode || s.workspace_mode === mode)
      return reply(rows, 200, { total: rows.length })
    }
    const session = sessions.find(s => path.endsWith(`/sessions/${s.id}`))
    if (session) return reply(session)
    if (path.endsWith('/agents')) return reply(agents)
    const agent = agents.find(a => path.endsWith(`/agents/${a.id}`))
    if (agent) return reply(agent)
    if (path.endsWith('/models')) return reply([
      { id: 'chat', name: 'Test Chat', type: 'KnowledgeQA', source: 'remote', parameters: { model: 'test' } },
      { id: 'chat-alt', name: 'Other Chat', type: 'KnowledgeQA', source: 'remote', parameters: { model: 'test' } },
      { id: 'rerank', name: 'Test Rerank', type: 'Rerank', source: 'remote', parameters: { model: 'test' } },
    ])
    return reply([])
  })
}

test('patent navigation filters history and shows the active search scope', async ({ page }, testInfo) => {
  await setup(page)
  await page.goto('/platform/patent')
  await expect(page).toHaveURL(/\/platform\/patent\/search$/)
  await expect(page.getByText('Find relevant patents and check the evidence.')).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Searching local knowledge bases' })).toBeVisible()
  await expect(page.getByTestId('patent-session-list')).toContainText('patent_search history')
  await expect(page.getByTestId('patent-session-list')).not.toContainText('patent_writing history')
  await page.getByRole('button', { name: 'Writing Guidance', exact: true }).click()
  await expect(page).toHaveURL(/\/platform\/patent\/writing$/)
  await expect(page.getByTestId('patent-session-list')).toContainText('patent_writing history')
  await expect(page.getByTestId('patent-session-list')).not.toContainText('patent_search history')
  await page.reload()
  await expect(page.getByText('Improve technical materials and identify writing gaps.')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('patent-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('button', { name: 'Patent Search', exact: true })).toBeVisible()
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await expect(page.getByText('Improve technical materials and identify writing gaps.')).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('patent-mobile.png') })
})

test('disabled patent workspace rejects direct navigation', async ({ page }) => {
  await setup(page, false)
  await page.goto('/platform/patent/writing')
  await expect(page).toHaveURL(/\/platform\/knowledge-bases$/)
  await expect(page.locator('.menu_item').filter({ hasText: 'Patent Workspace' })).toHaveCount(0)
})

test('composer profiles persist separately and leave ordinary settings intact', async ({ page }) => {
  await setup(page)
  await page.goto('/platform/patent/search')
  await expect(page.getByText('Find relevant patents and check the evidence.')).toBeVisible()
  const state = await page.evaluate(() => {
    const pinia = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia
    const settings = pinia._s.get('settings')
    const original = JSON.stringify(settings._patentSnapshot)
    settings.settings.selectedKnowledgeBases = ['search-kb']
    settings.settings.selectedSkills = ['search-skill']
    settings.settings.webSearchEnabled = true
    settings.persistComposerSettings()
    settings.enterPatentWorkspace('patent_writing')
    const writingStartsEmpty = settings.settings.selectedKnowledgeBases.length === 0 && !settings.settings.webSearchEnabled
    settings.settings.selectedKnowledgeBases = ['writing-kb']
    settings.settings.selectedMCPServices = ['writing-tool']
    settings.persistComposerSettings()
    settings.enterPatentWorkspace('patent_search')
    const search = settings.patentProfile()
    settings.enterPatentWorkspace('patent_writing')
    const writing = settings.patentProfile()
    settings.leavePatentWorkspace()
    return { writingStartsEmpty, search, writing, ordinaryRestored: JSON.stringify(settings.settings) === original, ordinaryStored: localStorage.getItem('WeKnora_settings') === original }
  })
  expect(state.writingStartsEmpty).toBe(true)
  expect(state.search.selectedKnowledgeBases).toEqual(['search-kb'])
  expect(state.search.selectedSkills).toEqual(['search-skill'])
  expect(state.search.webSearchEnabled).toBe(true)
  expect(state.writing.selectedKnowledgeBases).toEqual(['writing-kb'])
  expect(state.writing.selectedMCPServices).toEqual(['writing-tool'])
  expect(state.writing.webSearchEnabled).toBe(false)
  expect(state.ordinaryRestored).toBe(true)
  expect(state.ordinaryStored).toBe(true)
})

test('page navigation and refresh restore each composer profile', async ({ page }) => {
  await setup(page)
  await page.goto('/platform/patent/search')
  await expect(page.getByText('Find relevant patents and check the evidence.')).toBeVisible()
  await page.evaluate(() => {
    const settings = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('settings')
    settings.settings.webSearchEnabled = true
    settings.settings.selectedKnowledgeBases = ['search-kb']
    settings.settings.conversationModels.selectedChatModelId = 'chat-alt'
    localStorage.setItem('weknora_last_chat_model_id', 'chat')
    localStorage.setItem(`${settings._patentProfileKey}:last_chat_model`, 'chat-alt')
    settings.persistComposerSettings()
  })
  await expect(page.getByRole('status').filter({ hasText: 'Using local materials and web search' })).toBeVisible()
  await page.getByRole('button', { name: 'Writing Guidance', exact: true }).click()
  const webEnabled = () => page.evaluate(() => (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('settings').settings.webSearchEnabled)
  await expect.poll(webEnabled).toBe(false)
  await page.getByRole('button', { name: 'Patent Search', exact: true }).click()
  await expect.poll(webEnabled).toBe(true)
  await page.reload()
  await expect(page.getByText('Find relevant patents and check the evidence.')).toBeVisible()
  await expect.poll(webEnabled).toBe(true)
  const restored = await page.evaluate(() => {
    const settings = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('settings')
    return { kbs: settings.settings.selectedKnowledgeBases, model: settings.settings.conversationModels.selectedChatModelId, ordinaryModel: localStorage.getItem('weknora_last_chat_model_id') }
  })
  expect(restored).toEqual({ kbs: ['search-kb'], model: 'chat-alt', ordinaryModel: 'chat' })
})

test('first send enters patent history and wrong detail routes follow server ownership', async ({ page }) => {
  await setup(page)
  await page.goto('/platform/patent/search')
  await page.locator('.patent-workspace textarea').fill('Find patents about heat exchangers')
  await page.locator('.patent-workspace [data-guide="chat-send"]').click()
  await expect(page).toHaveURL(/\/platform\/patent\/search\/chat\/patent_search-new$/)
  await expect(page.getByTestId('patent-session-list')).toContainText('New Session')
  await page.goto('/platform/patent/writing/chat/patent_search-session')
  await expect(page).toHaveURL(/\/platform\/patent\/search\/chat\/patent_search-session$/)
  await expect(page.getByTestId('patent-session-list')).not.toContainText('patent_writing history')
})

test('patent history keeps its saved model when agent defaults arrive late', async ({ page }) => {
  await setup(page)
  await page.addInitScript(() => localStorage.setItem(
    'WeKnora_patent_profile:1:patent-user:patent_writing:last_chat_model', 'chat'))
  await page.route('**/api/v1/sessions/patent_writing-session', route => route.fulfill({ json: {
    success: true, data: {
      id: 'patent_writing-session', title: 'Saved writing model', workspace_mode: 'patent_writing',
      last_request_state: { agent_id: 'builtin-patent-writing', agent_enabled: true, model_id: 'chat-alt', web_search_enabled: false },
    },
  } }))
  await page.goto('/platform/patent/writing/chat/patent_writing-session')
  const model = () => page.evaluate(() => {
    const s = (document.querySelector('#app') as any)?.__vue_app__?.config.globalProperties.$pinia._s.get('settings')
    return s?.settings.conversationModels || null
  })
  await expect.poll(model).toMatchObject({ selectedChatModelId: 'chat-alt', summaryModelId: 'chat-alt' })
  await page.evaluate(() => {
    const resources = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('chatResources')
    resources.agents = resources.agents.map((a: any) => a.id === 'builtin-patent-writing'
      ? { ...a, config: { ...a.config, model_id: 'new-agent-default' } } : a)
  })
  await expect.poll(model).toMatchObject({ selectedChatModelId: 'chat-alt', summaryModelId: 'chat-alt' })
  await page.reload()
  await expect.poll(model).toMatchObject({ selectedChatModelId: 'chat-alt', summaryModelId: 'chat-alt' })
})

for (const feature of ['search', 'writing']) {
  test(`patent ${feature} first send preserves the draft model without a last-pick key`, async ({ page }) => {
    await setup(page)
    let sentModel: string | undefined
    await page.route('**/api/v1/agent-chat/**', async route => {
      sentModel = route.request().postDataJSON().summary_model_id
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' })
    })
    await page.goto(`/platform/patent/${feature}`)
    await expect(page.locator('.patent-workspace textarea')).toBeVisible()
    await page.evaluate(() => {
      const s = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('settings')
      s.updateConversationModels({ selectedChatModelId: 'chat-alt', summaryModelId: 'chat-alt' })
      s.persistComposerSettings()
      localStorage.removeItem(`${s._patentProfileKey}:last_chat_model`)
    })
    await page.locator('.patent-workspace textarea').fill('Review only the materials I provide')
    await page.locator('.patent-workspace [data-guide="chat-send"]').click()
    await expect(page).toHaveURL(new RegExp(`/platform/patent/${feature}/chat/patent_${feature}-new$`))
    await expect.poll(() => sentModel).toBe('chat-alt')
    await expect(page.locator('.patent-workspace .model-selector-name').first()).toContainText('Other Chat')
  })
}

test('late config responses cannot enable the next workspace', async ({ page }) => {
  await setup(page)
  let oldRequest: Route | undefined
  await page.route('**/api/v1/tenants/kv/patent-workspace-config', async route => {
    if (!oldRequest) { oldRequest = route; return }
    await route.fulfill({ json: { success: true, data: { enabled: false } } })
  })
  await page.goto('/platform/creatChat')
  await expect.poll(() => Boolean(oldRequest)).toBe(true)
  await page.evaluate(() => {
    (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('auth').selectedTenantId = 2
  })
  const config = () => page.evaluate(() => {
    const store = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('patentWorkspace')
    return { enabled: store.enabled, loaded: store.loaded }
  })
  await expect.poll(config).toEqual({ enabled: false, loaded: true })
  await oldRequest!.fulfill({ json: { success: true, data: { enabled: true } } })
  await expect.poll(config).toEqual({ enabled: false, loaded: true })
})

test('batch selection deletes only the current patent subfeature', async ({ page }) => {
  await setup(page)
  let deleted: string[] = []
  await page.route('**/api/v1/sessions/batch', async route => {
    deleted = route.request().postDataJSON().ids
    await route.fulfill({ json: { success: true } })
  })
  await page.goto('/platform/patent/search')
  await page.getByTestId('patent-session-list').locator('.submenu_item').hover()
  await page.getByTestId('patent-session-list').getByRole('button', { name: 'More conversation actions', exact: true }).click()
  await page.getByRole('button', { name: 'Batch Manage', exact: true }).click()
  const footer = page.getByTestId('session-batch-footer')
  await footer.getByText('Select All', { exact: true }).click()
  await footer.getByRole('button', { name: 'Delete Conversations(1)', exact: true }).click()
  await page.getByRole('button', { name: 'Delete Conversations', exact: true }).click()
  await expect.poll(() => deleted).toEqual(['patent_search-session'])
  await page.getByRole('button', { name: 'Writing Guidance', exact: true }).click()
  await expect(page.getByTestId('patent-session-list')).toContainText('patent_writing history')
  await expect(footer).toHaveCount(0)
})
