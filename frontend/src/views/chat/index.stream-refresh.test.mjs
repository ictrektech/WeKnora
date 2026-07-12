import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const source = readFileSync(join(import.meta.dirname, 'index.vue'), 'utf8')
const handlerSource = readFileSync(
  join(import.meta.dirname, '../../composables/useChatStreamHandler.ts'),
  'utf8',
)

test('completed quick answer references sync without page navigation', () => {
  assert.match(source, /const syncCompletedMessageReferences = \(message,\s*attempt = 0\) => \{/)
  assert.match(source, /const findFreshMessageForReferences = \(items,\s*message\) => \{/)
  assert.match(source, /if \(payload\?\.is_completed\) \{[\s\S]*syncCompletedMessageReferences\(message\)/)
})

test('pre-answer references are kept for the first answer row', () => {
  assert.match(handlerSource, /let pendingKnowledgeReferences: unknown\[\] = \[\]/)
  assert.match(handlerSource, /pendingKnowledgeReferences = refs\.slice\(\)[\s\S]*return undefined/)
  assert.match(handlerSource, /entry\.knowledge_references = pendingKnowledgeReferences\.slice\(\)/)
})

test('completed stream rows merge with refreshed history when ids drift', () => {
  assert.match(handlerSource, /const findCurrentTurnAssistantByContent = \(item: ChatMessage\) => \{/)
  assert.match(handlerSource, /if \(message\.role === 'user'\) break/)
  assert.match(handlerSource, /const existing = findExistingMessage\(item,\s*!isScrollType\)/)
  assert.match(handlerSource, /message = findCurrentTurnAssistantByContent\(\{\s*\.\.\.payload,\s*role: 'assistant',\s*\}\)/)
})
