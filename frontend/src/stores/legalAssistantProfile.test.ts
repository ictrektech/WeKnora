import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import test from 'node:test'
import assert from 'node:assert/strict'

test('legal assistant profile has isolated defaults and lifecycle actions', () => {
  const source = readFileSync(join(import.meta.dirname, 'settings.ts'), 'utf8')
  assert.match(source, /BUILTIN_LEGAL_ASSISTANT_ID/)
  assert.match(source, /selectedAgentId: BUILTIN_LEGAL_ASSISTANT_ID/)
  assert.match(source, /webSearchEnabled: false/)
  assert.match(source, /enterLegalAssistant\(\)/)
  assert.match(source, /leaveLegalAssistant\(\)/)
  for (const field of ['selectedKnowledgeBases', 'selectedFiles', 'selectedTags', 'selectedMCPServices', 'selectedSkills', 'conversationModels']) {
    assert.match(source, new RegExp(`profile\\.${field}`))
  }
})
