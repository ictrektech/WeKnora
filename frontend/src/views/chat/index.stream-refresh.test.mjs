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

test('completed assistant rows are deduped inside the current user turn', () => {
  assert.match(handlerSource, /const dedupeCurrentTurnCompletedAssistants = \(preferred\?: ChatMessage\) => \{/)
  assert.match(handlerSource, /const lastUserIndex = findLastUserMessageIndex\(\)/)
  assert.match(handlerSource, /const candidates: ChatMessage\[\] = \[\]/)
  assert.match(handlerSource, /if \(message\.role !== 'assistant'\) continue/)
  assert.match(handlerSource, /if \(message\.is_completed\) score \+= 1000/)
  assert.match(handlerSource, /mergeAssistantRuntimeState\(retained,\s*message\)/)
  assert.match(handlerSource, /messagesList\.splice\(i,\s*1\)/)
  assert.match(handlerSource, /message = dedupeCurrentTurnCompletedAssistants\(message\) \|\| message/)
  assert.match(handlerSource, /const retainedEntry = payload\.is_completed[\s\S]*dedupeCurrentTurnCompletedAssistants\(entry\) \|\| entry/)
  assert.match(handlerSource, /messagesList\.push\(\.\.\.processed\)[\s\S]*dedupeCurrentTurnCompletedAssistants\(\)/)
})

test('answer stream chunks merge snapshots without duplicating the same answer', () => {
  assert.match(handlerSource, /const mergeStreamText = \(currentValue: unknown,\s*incomingValue: unknown\) => \{/)
  assert.match(handlerSource, /if \(current === incoming\) return current/)
  assert.match(handlerSource, /if \(incoming\.startsWith\(current\)\) return incoming/)
  assert.match(handlerSource, /if \(current\.endsWith\(incoming\)\) return current/)
  assert.match(handlerSource, /answerEvent\.content = mergeStreamText\(answerEvent\.content,\s*data\.content\)/)
  assert.match(handlerSource, /if \(data\.content\) \{[\s\S]*\} else if \(!answerEvent\.content && message\.content/)
})

test('chat view renders a deduped message list', () => {
  assert.match(source, /v-for=\"\(session, index\) in renderedMessagesList\"/)
  assert.match(source, /const renderedMessagesList = computed\(\(\) => \{/)
  assert.match(source, /let currentTurnAssistantIndex = -1/)
  assert.match(source, /message\.role === 'assistant'[\s\S]*normalizeRenderedMessageContent\(message\.content\)/)
  assert.match(source, /if \(message\?\.is_completed\) score \+= 1000/)
  assert.match(source, /result\[currentTurnAssistantIndex\] = message/)
})
