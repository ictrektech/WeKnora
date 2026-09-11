import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import test from 'node:test'
import assert from 'node:assert/strict'

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
  assert.match(api, /legal-assistant\/sessions/)
  assert.match(home, /workspace-mode="legal_assistant"/)
  assert.match(creatChat, /legal-assistant\/chat\//)
  assert.match(creatChat, /workspace_mode: 'legal_assistant'/)
  assert.match(creatChat, /createLegalAssistantSession/)
})
