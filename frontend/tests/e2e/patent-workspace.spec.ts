import { expect, test, type Page, type Route } from '@playwright/test'
import { readFileSync } from 'node:fs'

const exampleCatalog = JSON.parse(readFileSync(new URL('../../../internal/patentexamples/data/catalog.json', import.meta.url), 'utf8'))

const timestamp = '2026-10-21T00:00:00Z'
async function setup(page: Page, enabled = true, role = 'owner') {
  const user = { id: 'patent-user', username: 'Patent User', email: 'patent@example.test', tenant_id: 1, is_active: true, created_at: timestamp, updated_at: timestamp }
  const tenant = { id: 1, name: 'Patent Test', owner_id: user.id, created_at: timestamp, updated_at: timestamp }
  const memberships = [{ tenant_id: 1, role }]
  const config = { enabled, examples_enabled: true }
  await page.addInitScript(({ user, tenant, memberships }) => {
    localStorage.setItem('weknora_token', 'patent-test-token')
    localStorage.setItem('weknora_selected_tenant_id', '1')
    localStorage.setItem('weknora_user', JSON.stringify(user))
    localStorage.setItem('weknora_tenant', JSON.stringify(tenant))
    localStorage.setItem('weknora_memberships', JSON.stringify(memberships))
    localStorage.setItem('locale', 'en-US')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
    localStorage.setItem('weknora:assistant-entry-guide-done:v0.9.0', '1')
  }, { user, tenant, memberships })
  const modes = ['platform', 'legal_assistant', 'patent_search', 'patent_writing']
  const sessions = modes.map(workspace_mode => ({ id: `${workspace_mode}-session`, title: `${workspace_mode} history`, workspace_mode, created_at: timestamp, updated_at: timestamp, user_id: user.id }))
  const agents = modes.map(mode => ({ id: mode === 'platform' ? 'builtin-quick-answer' : `builtin-${mode.replace('_', '-')}`, name: mode, is_builtin: true, config: { agent_mode: 'smart-reasoning', model_id: 'chat', rerank_model_id: 'rerank', web_search_enabled: true, kb_selection_mode: 'all', allowed_tools: ['search_knowledge', 'read_document'] } }))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    const reply = (data: unknown, status = 200, extra: Record<string, unknown> = {}) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ success: status < 400, data, ...extra }) })
    if (path.endsWith('/auth/me')) return reply({ user, tenant, memberships, capabilities: { can_create_tenant: false } })
    if (path.endsWith('/system/capabilities')) return reply({ edition: 'community', capabilities: {} })
    if (path.endsWith('/patent-workspace-config')) {
      if (route.request().method() === 'PUT') Object.assign(config, route.request().postDataJSON())
      return reply(config)
    }
    if (path.includes('/patent-workspace/examples')) {
      if (!config.enabled || !config.examples_enabled) return reply(null, 403)
      const suffix = path.split('/patent-workspace/examples')[1]
      if (!suffix) {
        const mode = new URL(route.request().url()).searchParams.get('workspace_mode')
        return reply({ version: exampleCatalog.version, examples: exampleCatalog.examples.filter((e: any) => !mode || e.workspace_mode === mode).map((e: any) => ({ ...e, messages: undefined })) })
      }
      const [, id, , docID] = suffix.split('/')
      const example = exampleCatalog.examples.find((e: any) => e.id === id)
      if (docID && example?.documents.some((doc: any) => doc.id === docID)) {
        const document = example.documents.find((doc: any) => doc.id === docID)
        return reply({ id: docID, filename: document.filename, document, content: document.publication ? '' : readFileSync(new URL(`../../../internal/patentexamples/data/${docID}.md`, import.meta.url), 'utf8') })
      }
      return example ? reply(example) : reply(null, 404)
    }
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
  return config
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

test('recorded examples stay read-only, offline and outside business and assistant context', async ({ page }, testInfo) => {
  await setup(page)
  const mutations: string[] = []
  const external: string[] = []
  page.on('request', req => {
    if (req.url().includes('/api/v1/') && !['GET', 'HEAD'].includes(req.method())) mutations.push(req.url())
    if (!new URL(req.url()).hostname.match(/^(127\.0\.0\.1|localhost)$/)) external.push(req.url())
  })
  await page.goto('/platform/patent/writing')
  const examples = page.getByTestId('patent-example-list')
  await expect(examples.getByText('Examples', { exact: true })).toBeVisible()
  await expect(page.getByTestId('patent-examples-entry')).toHaveCount(0)
  await examples.getByRole('button', { name: '批任务调度系统交底书提纲与缺漏检查', exact: true }).click()
  const view = page.getByTestId('patent-examples')
  await page.getByRole('button', { name: 'Example Details', exact: true }).click()
  await expect(page.getByText('workbuddy/deepseek-v4.1-flash', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Example Details', exact: true }).click()
  await expect(view.getByTestId('patent-example-message')).toHaveCount(4)
  await expect(view.locator('textarea, input[type=file]')).toHaveCount(0)
  await expect(page.locator('.chat-send-btn')).toHaveCount(0)
  await expect(view.locator('.bot_msg .answer-toolbar')).toHaveCount(0)
  await expect(view.locator('.chat-header__menu-btn')).toHaveCount(0)
  await view.locator('.chat-header__title').dblclick()
  await expect(view.locator('.chat-header__edit-input')).toHaveCount(0)
  await expect(page.getByTestId('patent-session-list')).not.toContainText('调度系统')
  const pageSnapshot = await page.evaluate(async () => {
    const { snapshotPage } = await import('/src/utils/pageSnapshot.ts')
    return snapshotPage().snapshot
  })
  expect(pageSnapshot).not.toContain('调度系统')
  expect(pageSnapshot).not.toContain('资源画像')
  await page.getByRole('button', { name: 'architecture.md', exact: true }).click()
  const preview = page.getByTestId('patent-example-preview')
  await expect(preview).toContainText('批任务调度架构（虚构示例）')
  await expect(preview).toContainText('分钟级')
  const snapshot = await page.evaluate(async () => {
    const { snapshotPage } = await import('/src/utils/pageSnapshot.ts')
    return snapshotPage().snapshot
  })
  expect(snapshot).not.toContain('批任务调度架构')
  expect(snapshot).not.toContain('资源画像聚合')
  await page.keyboard.press('Escape')
  await page.evaluate(() => {
    const el = document.querySelector('[data-testid="patent-example-message"] .user_msg')!
    const selection = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(el)
    selection.removeAllRanges()
    selection.addRange(range)
    document.dispatchEvent(new Event('selectionchange'))
  })
  await page.waitForTimeout(250)
  await expect.poll(() => page.evaluate(() => {
    const a = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('assistant')
    return a.pageSelection?.text || ''
  })).toBe('')
  await page.evaluate(() => {
    const selection = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(document.body)
    selection.removeAllRanges()
    selection.addRange(range)
    document.dispatchEvent(new Event('selectionchange'))
  })
  await page.waitForTimeout(250)
  expect(await page.evaluate(() => {
    return (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('assistant').pageSelection
  })).toBeNull()
  await page.reload()
  await expect(view.getByTestId('patent-example-message')).toHaveCount(4)
  await page.screenshot({ path: testInfo.outputPath('patent-example-desktop.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('patent-example-mobile.png') })
  expect(mutations).toEqual([])
  expect(external).toEqual([])
})

test('example visibility persists across reload and gates direct detail navigation', async ({ page }) => {
  const config = await setup(page)
  await page.goto('/platform/patent/writing/examples/writing-outline')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(4)
  await page.getByRole('button', { name: 'Open Example Settings', exact: true }).click()
  await page.getByTestId('patent-examples-switch').click()
  await expect.poll(() => config.examples_enabled).toBe(false)
  await expect(page.getByTestId('patent-example-message')).toHaveCount(0)
  await page.reload()
  await expect(page).toHaveURL(/\/platform\/patent\/writing$/)
  await expect(page.getByTestId('patent-example-list')).toHaveCount(0)
  await page.goto('/platform/settings?section=integration-patent')
  await page.getByTestId('patent-examples-switch').click()
  await expect.poll(() => config.examples_enabled).toBe(true)
  await page.goto('/platform/patent/writing/examples/writing-outline')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(4)
})

test('independent writing examples do not share attachments or search examples', async ({ page }) => {
  await setup(page, true, 'viewer')
  await page.goto('/platform/patent/writing/examples/writing-consistency')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(2)
  await expect(page.getByTestId('patent-examples').locator('.user_attachment_card')).toHaveCount(0)
  await expect(page.getByTestId('patent-examples').getByRole('button', { name: 'Open Example Settings' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'architecture.md', exact: true })).toHaveCount(0)
  await page.goto('/platform/patent/writing/examples/writing-no-material')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(2)
  await expect(page.getByTestId('patent-examples')).toContainText('尚未收到材料')
  await page.goto('/platform/patent/search/examples')
  await expect(page).toHaveURL(/\/platform\/patent\/search$/)
  await expect(page.getByTestId('patent-example-list').locator('.submenu_item')).toHaveCount(3)
  await expect(page.getByTestId('patent-example-list')).not.toContainText('调度系统')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(0)
})

test('a late recording response cannot cross workspace changes', async ({ page }) => {
  await setup(page)
  let pending: Route | undefined
  await page.route('**/api/v1/patent-workspace/examples/writing-outline', route => { pending = route })
  await page.goto('/platform/patent/writing/examples/writing-outline')
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.route('**/api/v1/tenants/kv/patent-workspace-config', route => route.fulfill({ json: { success: true, data: { enabled: true, examples_enabled: false } } }))
  await page.evaluate(() => {
    (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('auth').selectedTenantId = 2
  })
  await expect(page.getByTestId('patent-examples')).toContainText('Examples are disabled')
  await pending!.fulfill({ json: { success: true, data: exampleCatalog.examples[0] } })
  await expect(page.getByTestId('patent-example-message')).toHaveCount(0)
})

test('example read failures can be retried without creating conversations', async ({ page }) => {
  await setup(page)
  let fail = true
  await page.route('**/api/v1/patent-workspace/examples/writing-no-material', route => route.fulfill({ status: fail ? 500 : 200, json: fail ? { success: false } : { success: true, data: exampleCatalog.examples.find((e: any) => e.id === 'writing-no-material') } }))
  await page.goto('/platform/patent/writing/examples/writing-no-material')
  await expect(page.getByTestId('patent-examples').getByRole('alert')).toContainText('Please retry')
  fail = false
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByTestId('patent-example-message')).toHaveCount(2)
})

test('closing an example preview discards the late original response', async ({ page }) => {
  await setup(page)
  let pending: Route | undefined
  await page.route('**/api/v1/patent-workspace/examples/writing-outline/documents/architecture', route => { pending = route })
  await page.goto('/platform/patent/writing/examples/writing-outline')
  await page.getByRole('button', { name: 'architecture.md', exact: true }).click()
  await expect(page.getByTestId('patent-example-preview')).toContainText('Loading examples')
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('patent-example-preview')).toHaveCount(0)
  const completed = page.waitForResponse(r => r.url().endsWith('/documents/architecture'))
  await pending!.fulfill({ json: { success: true, data: { id: 'architecture', filename: 'architecture.md', content: 'Late original must not reopen preview' } } })
  await completed
  await expect(page.getByTestId('patent-example-preview')).toHaveCount(0)
  await expect(page.getByText('Late original must not reopen preview')).toHaveCount(0)
})

test('focus refresh does not invalidate an in-flight example preference save', async ({ page }) => {
  await setup(page)
  let pending: Route | undefined
  await page.route('**/api/v1/tenants/kv/patent-workspace-config', async route => {
    if (route.request().method() === 'PUT') { pending = route; return }
    await route.fallback()
  })
  await page.goto('/platform/patent/writing/examples/writing-outline')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(4)
  await page.getByRole('button', { name: 'Open Example Settings', exact: true }).click()
  await page.getByTestId('patent-examples-switch').click()
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await pending!.fulfill({ json: { success: true, data: { enabled: true, examples_enabled: false } } })
  await expect.poll(() => page.evaluate(() => {
    const s = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('patentWorkspace')
    return { saving: s.saving, examplesEnabled: s.examplesEnabled }
  })).toEqual({ saving: false, examplesEnabled: false })
})

test('examples share history row styling but never join real batch selections', async ({ page }) => {
  await setup(page)
  let deleted: string[] = []
  await page.route('**/api/v1/sessions/batch', async route => {
    deleted = route.request().postDataJSON().ids
    await route.fulfill({ json: { success: true } })
  })
  await page.goto('/platform/patent/writing')
  const examples = page.getByTestId('patent-example-list')
  await expect(examples.locator('.submenu_item')).toHaveCount(3)
  await expect(examples.locator('.menu-more-wrap')).toHaveCount(0)
  const history = page.getByTestId('patent-session-list')
  await expect(history.locator('.submenu_item')).toHaveCount(1)
  expect(await history.evaluate((el) => Boolean(el.nextElementSibling?.matches('[data-testid="patent-example-list"]')))).toBe(true)
  await history.locator('.submenu_item').hover()
  await history.getByRole('button', { name: 'More conversation actions', exact: true }).click()
  await page.getByRole('button', { name: 'Batch Manage', exact: true }).click()
  const footer = page.getByTestId('session-batch-footer')
  await footer.getByText('Select All', { exact: true }).click()
  await expect(examples.locator('.t-checkbox')).toHaveCount(0)
  await footer.getByRole('button', { name: 'Delete Conversations(1)', exact: true }).click()
  await page.getByRole('button', { name: 'Delete Conversations', exact: true }).click()
  await expect.poll(() => deleted).toEqual(['patent_writing-session'])
  await examples.getByRole('button', { name: '批任务调度系统交底书提纲与缺漏检查', exact: true }).click()
  await expect(page).toHaveURL(/\/writing\/examples\/writing-outline$/)
  await expect(examples.locator('[aria-current="page"]')).toContainText('批任务调度系统交底书提纲与缺漏检查')
})

test('new users can open examples from empty history and attachment cards support keyboard preview', async ({ page }) => {
  await setup(page)
  await page.route(/\/api\/v1\/sessions(?:\?.*)?$/, route => route.fulfill({ json: { success: true, data: [], total: 0 } }))
  await page.goto('/platform/patent/writing')
  await expect(page.getByTestId('patent-session-list')).toHaveCount(0)
  const row = page.getByTestId('patent-example-list').getByRole('button', { name: '批任务调度系统交底书提纲与缺漏检查', exact: true })
  await row.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('patent-example-message')).toHaveCount(4)
  await expect(page.locator('.bot_msg .agent-stream-display .answer-content')).toHaveCount(2)
  await expect(page.locator('.streaming-answer-text')).toHaveCount(0)
  const attachment = page.getByRole('button', { name: 'architecture.md', exact: true })
  await attachment.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('patent-example-preview')).toContainText('批任务调度架构')
})

test('sidebar examples stay outside page context while real chat remains usable', async ({ page }) => {
  await setup(page)
  await page.goto('/platform/patent/writing')
  await expect(page.getByTestId('patent-example-list').locator('.submenu_item')).toHaveCount(3)
  const snapshot = await page.evaluate(async () => {
    const { snapshotPage } = await import('/src/utils/pageSnapshot.ts')
    return snapshotPage().snapshot
  })
  expect(snapshot).not.toContain('调度系统')
  await page.evaluate(() => {
    const selection = window.getSelection()!
    const range = document.createRange()
    range.selectNodeContents(document.body)
    selection.removeAllRanges()
    selection.addRange(range)
    document.dispatchEvent(new Event('selectionchange'))
  })
  await page.waitForTimeout(250)
  const selected = await page.evaluate(() => {
    return (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('assistant').pageSelection?.text || ''
  })
  expect(selected).not.toContain('调度系统')
  expect(selected).not.toContain('人工反例')
  await page.locator('.patent-workspace textarea').fill('My own technical material')
  await expect(page.locator('.patent-workspace textarea')).toHaveValue('My own technical material')
})

test('late sidebar responses cannot bring writing examples into search', async ({ page }) => {
  await setup(page)
  let pending: Route | undefined
  await page.route('**/api/v1/patent-workspace/examples?workspace_mode=patent_writing', route => { pending = route })
  await page.goto('/platform/patent/writing')
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.getByRole('button', { name: 'Patent Search', exact: true }).click()
  await expect(page.getByTestId('patent-example-list')).toContainText('政法大模型问答系统专利检索')
  const completed = page.waitForResponse(r => r.url().endsWith('workspace_mode=patent_writing'))
  await pending!.fulfill({ json: { success: true, data: exampleCatalog } })
  await completed
  await page.waitForTimeout(150)
  await expect(page.getByTestId('patent-example-list').locator('.submenu_item')).toHaveCount(3)
  await expect(page.getByTestId('patent-example-list')).not.toContainText('调度系统')
})

test('sidebar failures allow retry and old gallery bookmarks return to the workspace', async ({ page }) => {
  await setup(page)
  let fail = true
  await page.route('**/api/v1/patent-workspace/examples?workspace_mode=patent_writing', route => route.fulfill({ status: fail ? 500 : 200, json: fail ? { success: false } : { success: true, data: exampleCatalog } }))
  await page.goto('/platform/patent/writing/examples')
  await expect(page).toHaveURL(/\/platform\/patent\/writing$/)
  await expect(page.getByTestId('patent-example-list').getByRole('alert')).toContainText('Please retry')
  fail = false
  await page.getByTestId('patent-example-list').getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByTestId('patent-example-list').locator('.submenu_item')).toHaveCount(3)
  await expect(page.getByTestId('patent-examples-entry')).toHaveCount(0)
})

for (const [id, title, numbers] of [
  ['search-political-qa', '政法大模型问答系统专利检索', ['CN119961390A']],
  ['search-tax-agent', '税务 AI 智能体专利检索', ['CN119961396A']],
  ['search-topic', '行业知识库与大模型问答相关专利检索', ['CN119961390A', 'CN119961396A']],
] as const) {
  test(`search recording ${id} opens its isolated example library and source locations`, async ({ page }, testInfo) => {
    await setup(page, true, 'viewer')
    const forbidden: string[] = []
    page.on('request', request => {
      const url = new URL(request.url())
      if (!['GET', 'HEAD'].includes(request.method()) || !['localhost', '127.0.0.1'].includes(url.hostname)
        || (url.pathname.startsWith('/api/v1/') && /\/sessions\/search-(political-qa|tax-agent|topic)|temporary-documents|\/knowledge\/|\/chunks\//.test(url.pathname))) forbidden.push(request.url())
    })
    await page.goto('/platform/patent/search')
    await page.getByTestId('patent-example-list').getByRole('button', { name: title, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/search/examples/${id}$`))
    const view = page.getByTestId('patent-examples')
    await expect(view.getByTestId('patent-example-message')).toHaveCount(2)
    await expect(view.getByTestId('patent-example-knowledge')).toBeVisible()
    await expect(view.locator('.example-knowledge')).toHaveCount(0)
    await expect(view.locator('.agent-stream-display')).toHaveCount(1)
    await expect(view.locator('textarea, input[type=file], .answer-toolbar, .user_attachment_card')).toHaveCount(0)
    await expect(page.getByTestId('patent-session-list')).not.toContainText(title)
    await view.getByRole('button', { name: /Example Knowledge Base/ }).click()
    const library = page.getByTestId('patent-example-library')
    await expect(library.locator('.example-document-row')).toHaveCount(numbers.length)
    await expect(library.locator('input, .menu-more-wrap')).toHaveCount(0)
    for (const number of numbers) {
      await library.getByRole('button', { name: new RegExp(number) }).click()
      const doc = page.getByTestId('patent-example-publication')
      await expect(doc).toContainText(number)
      await expect(doc).toContainText('2025-05-09')
      const external = doc.getByRole('link', { name: 'View Original at Source (Online)' })
      await expect(external).toHaveAttribute('href', `https://patents.google.com/patent/${number}/zh`)
      await expect(external).toHaveAttribute('target', '_blank')
      await expect(external).toHaveAttribute('rel', 'noopener noreferrer')
      await expect(page.locator('iframe, embed, object')).toHaveCount(0)
      await page.getByRole('button', { name: 'Back to Documents', exact: true }).click()
    }
    await page.keyboard.press('Escape')
    const citation = view.locator('a[href^="#example-source/"]').first()
    const href = await citation.getAttribute('href')
    await expect(citation).toHaveClass(/citation-example/)
    await expect(citation).toContainText(`${numbers[0]}.pdf`)
    await expect(view.locator('.citation-kb')).toHaveCount(0)
    await citation.hover()
    const popup = page.locator('.chat-citation-float[data-assistant-ignore]')
    await expect(popup).toContainText(`${numbers[0]}.pdf`)
    await expect(popup).toContainText('Editorial Summaries')
    expect(await page.evaluate(async () => (await import('/src/utils/pageSnapshot.ts')).snapshotPage().snapshot)).not.toContain(numbers[0])
    await citation.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByTestId('patent-example-publication')).toContainText(numbers[0])
    await expect(page.locator('.evidence-summary.is-active')).toHaveAttribute('data-evidence-id', href!.split('/')[2]!)
    const panel = page.getByTestId('patent-example-source-panel')
    await expect(panel).toBeVisible()
    await expect(view.locator('.example-main')).toBeVisible()
    await expect(view.locator('.t-drawer')).toHaveCount(0)
    await view.getByRole('button', { name: 'Example Details', exact: true }).click({ trial: true })
    const mainBounds = await view.locator('.example-main').boundingBox()
    const panelBounds = await panel.boundingBox()
    expect(mainBounds!.x + mainBounds!.width).toBeLessThanOrEqual(panelBounds!.x + 1)
    await expect(popup).toHaveCount(0)
    if (numbers.length === 2) {
      await view.locator(`a[href="#example-source/${numbers[1]}/abstract"]`).first().click()
      await expect(page.getByTestId('patent-example-publication')).toContainText(numbers[1])
      await expect(page.locator('.evidence-summary.is-active')).toHaveAttribute('data-evidence-id', 'abstract')
    }
    await page.screenshot({ path: testInfo.outputPath(`${id}-source-panel.png`) })
    await expect(page.getByTestId('patent-example-preview')).toContainText('no full PDFs')
    await expect(page.getByTestId('patent-example-preview')).not.toContainText('Full text requires visiting an external source online.')
    await expect(page.getByTestId('patent-example-preview')).not.toContainText('No applicant endorsement is implied.')
    expect(await page.evaluate(async () => (await import('/src/utils/pageSnapshot.ts')).snapshotPage().snapshot)).not.toContain(numbers[0])
    await page.keyboard.press('Escape')
    await page.reload()
    await expect(view.getByTestId('patent-example-message')).toHaveCount(2)
    await page.screenshot({ path: testInfo.outputPath(`${id}-desktop.png`) })
    await page.setViewportSize({ width: 390, height: 844 })
    await view.getByRole('button', { name: /Example Knowledge Base/ }).click()
    await library.getByRole('button', { name: new RegExp(numbers[0]) }).click()
    await expect(page.getByTestId('patent-example-publication')).toBeVisible()
    await expect(view.locator('.example-main')).toBeHidden()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`${id}-mobile.png`) })
    await panel.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(panel).toHaveCount(0)
    await expect(view.locator('.example-main')).toBeVisible()
    expect(forbidden).toEqual([])
  })
}

test('late publication details cannot cross examples and forged citations cannot cross document scope', async ({ page }) => {
  await setup(page)
  let pending: Route | undefined
  await page.route('**/api/v1/patent-workspace/examples/search-political-qa/documents/CN119961390A', route => { pending = route })
  await page.goto('/platform/patent/search/examples/search-political-qa')
  const link = page.locator('a[href^="#example-source/"]').first()
  await link.evaluate(el => el.setAttribute('href', '#example-source/CN119961396A/abstract'))
  await link.click()
  await expect(page.getByTestId('patent-example-preview')).not.toBeVisible()
  expect(pending).toBeUndefined()
  await page.getByRole('button', { name: /Example Knowledge Base/ }).click()
  await page.getByTestId('patent-example-library').getByRole('button', { name: /CN119961390A/ }).click()
  await expect.poll(() => Boolean(pending)).toBe(true)
  await page.keyboard.press('Escape')
  await page.getByTestId('patent-example-list').getByRole('button', { name: '税务 AI 智能体专利检索', exact: true }).click()
  await expect(page.getByTestId('patent-example-message').first()).toContainText('CN119961396A')
  const document = exampleCatalog.examples.find((e: any) => e.id === 'search-political-qa').documents[0]
  await pending!.fulfill({ json: { success: true, data: { id: document.id, filename: document.filename, document, content: '' } } })
  await expect(page.getByTestId('patent-example-preview')).toHaveCount(0)
  await page.getByRole('button', { name: /Example Knowledge Base/ }).click()
  await expect(page.getByTestId('patent-example-library')).not.toContainText('CN119961390A')
})

test('source panel failures retry locally and disabling examples clears both sources and hover', async ({ page }) => {
  const config = await setup(page)
  let fail = true
  const document = exampleCatalog.examples.find((e: any) => e.id === 'search-political-qa').documents[0]
  await page.route('**/api/v1/patent-workspace/examples/search-political-qa/documents/CN119961390A', route => route.fulfill({
    status: fail ? 503 : 200,
    json: fail ? { success: false } : { success: true, data: { id: document.id, filename: document.filename, document, content: '' } },
  }))
  await page.goto('/platform/patent/search/examples/search-political-qa')
  const citation = page.locator('.citation-example').first()
  await citation.click()
  const panel = page.getByTestId('patent-example-source-panel')
  await expect(panel.getByRole('alert')).toContainText('Please retry')
  fail = false
  await panel.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByTestId('patent-example-publication')).toContainText('CN119961390A')
  await citation.hover()
  await expect(page.locator('.chat-citation-float[data-assistant-ignore]')).toBeVisible()
  config.examples_enabled = false
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(page.getByTestId('patent-examples')).toContainText('Examples are disabled')
  await expect(panel).toHaveCount(0)
  await expect(page.locator('.chat-citation-float[data-assistant-ignore]')).toHaveCount(0)
  await expect(page.locator('.citation-example')).toHaveCount(0)
})

for (const id of ['search-political-qa', 'search-tax-agent', 'search-topic', 'writing-outline', 'writing-consistency', 'writing-no-material']) {
  test(`acceptance snapshot ${id} uses the original complete conversation and recorded tool timeline`, async ({ page }) => {
    await setup(page)
    const recording = exampleCatalog.examples.find((example: any) => example.id === id)
    const mode = recording.workspace_mode === 'patent_search' ? 'search' : 'writing'
    await page.goto(`/platform/patent/${mode}/examples/${id}`)
    const view = page.getByTestId('patent-examples')
    await expect(view.locator('.chat-header__title')).toHaveText(recording.title)
    const answers = recording.messages.filter((message: any) => message.role === 'assistant')
    const agents = view.locator('.agent-stream-display')
    await expect(agents).toHaveCount(answers.length)
    for (let index = 0; index < answers.length; index++) {
      const calls = answers[index].events.filter((event: any) => event.type === 'tool_call')
      const agent = agents.nth(index)
      await expect(agent.locator('.answer-content')).toBeVisible()
      if (!calls.length) {
        await expect(agent.locator('.tree-root')).toHaveCount(0)
        continue
      }
      await expect(agent.locator('.tree-root-summary')).toContainText(String(calls.length))
      await agent.locator('.tree-root').click()
      await expect(agent.locator('.tree-children .tool-event')).toHaveCount(calls.length)
    }
    if (id === 'search-political-qa' || id === 'search-tax-agent') {
      await expect(agents.locator('.answer-content ol > li')).toHaveCount(5)
      await expect(agents.locator('.answer-content')).toContainText('证据范围')
    }
    await expect(view.locator('textarea, input[type=file], .answer-toolbar')).toHaveCount(0)
    await expect(view).not.toContainText('核心技术方案（节选）')
    await page.reload()
    await expect(view.getByTestId('patent-example-message')).toHaveCount(recording.messages.length)
    await expect(agents).toHaveCount(answers.length)
  })
}
