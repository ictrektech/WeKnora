import { expect, test } from '@playwright/test'
import type { Route } from '@playwright/test'

const timestamp = '2026-08-19T00:00:00Z'

const documents = [
  {
    id: 'archive-1',
    title: 'Agreement One',
    file_name: 'agreement-one.pdf',
    file_type: 'pdf',
    file_size: 1024,
    document_type: 'contract',
    business_type: '',
    agreement_number: 'AG-001',
    amount: 0,
    currency: '',
    extracted_fields: {},
    extraction_status: 'completed',
    created_at: timestamp,
    updated_at: timestamp,
  },
  {
    id: 'archive-failed',
    title: 'Agreement That Needs Review',
    file_name: 'agreement-failed.pdf',
    file_type: 'pdf',
    file_size: 2048,
    document_type: 'contract',
    business_type: '',
    agreement_number: 'AG-002',
    amount: 0,
    currency: '',
    extracted_fields: {},
    extraction_status: 'needs_review',
    error_message: 'Needs manual confirmation',
    created_at: timestamp,
    updated_at: timestamp,
  },
]

function json(route: Route, data: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

test('loads documents while settings are blocked and retains partial bulk failures', async ({ page }) => {
  let bulkActionDone = false
  let settingsReleased = false
  let releaseSettings!: () => void
  const settingsGate = new Promise<void>((resolve) => { releaseSettings = () => { settingsReleased = true; resolve() } })
  const searchBodies: Array<{ query?: string }> = []

  await page.addInitScript(() => {
    const user = {
      id: 'e2e-user',
      username: 'Archive Contributor',
      email: 'archive@example.test',
      tenant_id: 1,
      is_active: true,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    const tenant = {
      id: 1,
      name: 'Archive Test Workspace',
      owner_id: 'e2e-user',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    localStorage.setItem('weknora_token', 'e2e-token')
    localStorage.setItem('weknora_selected_tenant_id', '1')
    localStorage.setItem('weknora_user', JSON.stringify(user))
    localStorage.setItem('weknora_tenant', JSON.stringify(tenant))
    localStorage.setItem('weknora_memberships', JSON.stringify([{ tenant_id: 1, role: 'contributor' }]))
    localStorage.setItem('locale', 'en-US')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
  })

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (path.endsWith('/auth/me')) {
      return json(route, {
        success: true,
        data: {
          user: {
            id: 'e2e-user',
            username: 'Archive Contributor',
            email: 'archive@example.test',
            tenant_id: 1,
            is_active: true,
            created_at: timestamp,
            updated_at: timestamp,
          },
          tenant: {
            id: 1,
            name: 'Archive Test Workspace',
            owner_id: 'e2e-user',
            created_at: timestamp,
            updated_at: timestamp,
          },
          memberships: [{ tenant_id: 1, role: 'contributor' }],
          capabilities: { can_create_tenant: false, auto_accept_invitation: false },
        },
      })
    }
    if (path.endsWith('/system/capabilities')) {
      return json(route, { success: true, data: { edition: 'community', capabilities: {} } })
    }
    if (path.endsWith('/tenants/kv/legal-workspace-config')) {
      return json(route, { success: true, data: { enabled: true } })
    }
    if (path.endsWith('/archive/settings')) {
      await settingsGate
      return json(route, { success: false, message: 'settings unavailable' }, 503)
    }
    if (path.endsWith('/tenants/1/members')) {
      return json(route, {
        success: true,
        data: {
          members: [{ user_id: 'e2e-user', email: 'archive@example.test', username: 'Archive Contributor', role: 'contributor', status: 'active', joined_at: timestamp }],
          total: 1,
        },
      })
    }
    if (path.endsWith('/archive/search') && request.method() === 'POST') {
      const body = JSON.parse(request.postData() || '{}') as { query?: string }
      searchBodies.push(body)
      return json(route, {
        success: true,
        data: {
          answer: '',
          documents: bulkActionDone ? [documents[1]] : documents,
          customers: [],
          citations: [],
          total: bulkActionDone ? 1 : 2,
        },
      })
    }
    if (path.endsWith('/archive/documents/archive-1')) {
      return json(route, { success: true, data: { ...documents[0], extraction_progress: 100 } })
    }
    if (path.endsWith('/archive/documents/bulk/archive') && request.method() === 'POST') {
      bulkActionDone = true
      return json(route, {
        success: true,
        data: {
          action: 'archive',
          requested: 2,
          succeeded: 1,
          failed: 1,
          items: [
            { id: 'archive-1', success: true },
            { id: 'archive-failed', success: false, error: 'document needs review' },
          ],
        },
      })
    }
    if (path.endsWith('/archive/reminders') || path.endsWith('/archive/reminder-candidates') || path.endsWith('/archive/notifications')) {
      return json(route, { success: true, data: [] })
    }
    // Menu, invitation and optional workspace widgets are outside this mock's
    // scope; an empty success response lets the archive route render without a
    // real backend while preserving the request boundary.
    return json(route, { success: true, data: [] })
  })

  await page.goto('/platform/smart-archive')
  await expect(page.getByRole('heading', { name: 'Contract Archive' })).toBeVisible()
  await expect(page.getByTestId('archive-row-archive-1')).toBeVisible()
  await expect(page.getByTestId('archive-row-archive-failed')).toBeVisible()
  expect(settingsReleased).toBe(false)
  releaseSettings()

  await page.getByTestId('archive-row-archive-1').click()
  const stageCard = page.getByTestId('archive-detail-stages')
  await expect(stageCard).toBeVisible()
  const stages = stageCard.getByRole('listitem')
  await expect(stages).toHaveCount(5)
  await expect(stages).toContainText(['Queued', 'Processing content', 'Extracting fields', 'Linking', 'Finalizing'])
  await expect(stages).toHaveText(['QueuedCompleted', 'Processing contentCompleted', 'Extracting fieldsCompleted', 'LinkingCompleted', 'FinalizingCompleted'])
  await expect(stageCard).not.toContainText(/\d+%/)
  await page.locator('.t-drawer__close-btn').click()

  await page.getByTestId('archive-search').fill('AG-001')
  await page.getByTestId('archive-search').press('Enter')
  await expect.poll(() => searchBodies.at(-1)?.query).toBe('AG-001')

  page.on('dialog', (dialog) => void dialog.accept())
  await page.getByTestId('archive-select-all').click()
  await page.getByTestId('archive-bulk-archive').click()
  await expect(page.getByTestId('archive-row-archive-failed')).toBeVisible()
  await expect(page.getByTestId('archive-row-archive-1')).not.toBeVisible()
})
