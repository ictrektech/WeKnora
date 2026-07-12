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
  assert.match(handlerSource, /const mergeHistoryMessage = \(existing: ChatMessage, item: ChatMessage\) => \{/)
  assert.match(handlerSource, /message = findCurrentTurnAssistantByContent\(\{\s*\.\.\.payload,\s*role: 'assistant',\s*\}\)/)
})

test('history refresh preserves active stream ids and later chunks target that row', () => {
  assert.match(handlerSource, /const streamId = existing\.id/)
  assert.match(handlerSource, /const streamRequestId = existing\.request_id/)
  assert.match(handlerSource, /if \(streamId\) existing\.id = streamId/)
  assert.match(handlerSource, /if \(streamRequestId\) existing\.request_id = streamRequestId/)
  assert.match(handlerSource, /const activeAssistantMessageId = currentAssistantMessageId\.value/)
  assert.match(handlerSource, /item\.id === activeAssistantMessageId[\s\S]*item\.request_id === activeAssistantMessageId/)
})
