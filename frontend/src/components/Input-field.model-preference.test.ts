import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { safeGetItem, safeSetItem, safeRemoveItem, userKey } from '../composables/preferenceStorage'

test('chat model preferences follow the active user and preserve separate patent profiles', t => {
  const items = new Map<string, string>()
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  t.after(() => {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else delete (globalThis as any).localStorage
  })
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => { items.set(key, value) },
    removeItem: (key: string) => { items.delete(key) },
  } })
  const source = readFileSync(new URL('./Input-field.vue', import.meta.url), 'utf8')
  const start = source.indexOf('const LAST_CHAT_MODEL_LEGACY_KEY = ')
  const end = source.indexOf('// Initial chat-model selection priority:', start)
  assert(start >= 0 && end > start)
  const settingsStore = { _patentMode: false, _patentProfileKey: '' }
  const { readLastChatModelID: read, writeLastChatModelID: write } = new Function(
    'settingsStore', 'safeGetItem', 'safeSetItem', 'safeRemoveItem', 'userKey',
    source.slice(start, end).replaceAll(': string', '') + '\nreturn { readLastChatModelID, writeLastChatModelID };',
  )(settingsStore, safeGetItem, safeSetItem, safeRemoveItem, userKey)
  items.set('weknora_user', JSON.stringify({ id: 'u1' }))
  items.set('weknora_last_chat_model_id', 'legacy')
  write('chat-u1')
  assert.equal(items.has('weknora_last_chat_model_id'), false)
  settingsStore._patentMode = true
  settingsStore._patentProfileKey = 'WeKnora_patent_profile:1:u1:patent_search'
  assert.equal(read(), '')
  write('patent-u1')
  settingsStore._patentMode = false
  assert.equal(read(), 'chat-u1')
  items.set('weknora_user', JSON.stringify({ id: 'u2' }))
  assert.equal(read(), '')
  write('chat-u2')
  write('')
  assert.equal(read(), '')
  items.set('weknora_user', JSON.stringify({ id: 'u1' }))
  assert.equal(read(), 'chat-u1')
  settingsStore._patentMode = true
  assert.equal(read(), 'patent-u1')
})
