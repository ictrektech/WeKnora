import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const chat = readFileSync(new URL('./index.vue', import.meta.url), 'utf8')
const botMessage = readFileSync(new URL('./components/botmsg.vue', import.meta.url), 'utf8')

test('chat cleanup has no references to the removed minimap flash state', () => {
  assert.doesNotMatch(chat, /\bclearMinimapFlash\b/)
  assert.doesNotMatch(chat, /\bminimapTargetId\b/)
})

test('bot message defines props before the citation hook reads them', () => {
  const propsIndex = botMessage.indexOf('const props = defineProps(')
  const citationHookIndex = botMessage.indexOf('useChatCitationPopover(parentMd')

  assert.notEqual(propsIndex, -1)
  assert.notEqual(citationHookIndex, -1)
  assert.ok(propsIndex < citationHookIndex)
})
