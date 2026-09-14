import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import test from 'node:test'
import assert from 'node:assert/strict'
import { isLegalAssistantRouteName, isLegalWorkspaceRouteName } from './paths'

const here = join(import.meta.dirname, '..')

test('legal assistant exposes guarded home and chat routes under the platform shell', () => {
  const source = readFileSync(join(here, 'router/index.ts'), 'utf8')
  assert.match(source, /path: "legal-assistant"/)
  assert.match(source, /name: "legalAssistantHome"/)
  assert.match(source, /path: "chat\/:chatid"/)
  assert.match(source, /name: "legalAssistantChat"/)
  assert.match(source, /requiresLegalWorkspace: true/)
})

test('legal assistant creation uses the dedicated endpoint and preserves the legal chat path', () => {
  const api = readFileSync(join(here, 'api/chat/index.ts'), 'utf8')
  const home = readFileSync(join(here, 'views/legal/LegalAssistantHome.vue'), 'utf8')
  const creatChat = readFileSync(join(here, 'views/creatChat/creatChat.vue'), 'utf8')
  const menu = readFileSync(join(here, 'components/menu.vue'), 'utf8')
  assert.match(api, /legal-assistant\/sessions/)
  assert.match(home, /workspace-mode="legal_assistant"/)
  assert.match(creatChat, /legal-assistant\/chat\//)
  assert.match(creatChat, /legalAssistant\.homeGreeting/)
  assert.match(creatChat, /workspace_mode: 'legal_assistant'/)
  assert.match(creatChat, /createLegalAssistantSession/)
  assert.match(creatChat, /notifySessionCreated\(obj\)/)
  assert.match(menu, /data-testid="legal-session-list"/)
  assert.match(menu, /<SessionSidebarRow :item="session"/)
  assert.match(menu, /data-testid="session-batch-footer"/)
  assert.match(menu, /session\.workspace_mode !== 'legal_assistant'/)
  assert.doesNotMatch(menu, /deleteAllSessions\(\)/)
  assert.match(menu, /isLegalWorkspaceRouteName\(route\.name\)/)
  assert.match(menu, /detail\.created && detail\.session/)
  assert.match(menu, /pendingCreatedSessions/)
})

test('legal route predicates are shared by the legal workspace shell', () => {
  assert.equal(isLegalAssistantRouteName('legalAssistant'), true)
  assert.equal(isLegalAssistantRouteName('legalAssistantHome'), true)
  assert.equal(isLegalAssistantRouteName('legalAssistantChat'), true)
  assert.equal(isLegalAssistantRouteName('legalSmartArchive'), false)
  assert.equal(isLegalWorkspaceRouteName('legalContractReview'), true)
  assert.equal(isLegalWorkspaceRouteName('legalContractReviewDetail'), true)
  assert.equal(isLegalWorkspaceRouteName('legalSmartArchive'), true)
  assert.equal(isLegalWorkspaceRouteName('legalAssistantChat'), true)
  assert.equal(isLegalWorkspaceRouteName('chat'), false)
})

test('session sidebar rows remain keyboard accessible after sharing the legal row', () => {
  const row = readFileSync(join(here, 'components/SessionSidebarRow.vue'), 'utf8')
  assert.match(row, /role="button" :tabindex="titleEditing \? -1 : 0"/)
  assert.match(row, /@keydown="handleRowKeydown"/)
  assert.match(row, /event\.key !== 'Enter' && event\.key !== ' '/)
})
