import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { createServer, type ViteDevServer } from 'vite'

let server: ViteDevServer
let bind: typeof import('./index').bindVOSIdentity
let base: { state: { value: string } }
const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
const originalFetch = globalThis.fetch

before(async () => {
  const mocks: Record<string, string> = {
    '@/utils/request': 'export const post=()=>{throw new Error("generic authentication retry is forbidden")};export const get=post;export const put=post;',
    '@/i18n': 'export default {global:{t:(key)=>key}}',
    '@/utils/api-base': 'export const state={value:"/app/com.ictrek.hybrag"};export const getApiBaseUrl=()=>state.value;',
  }
  server = await createServer({ configFile: false, plugins: [{ name: 'identity-binding-contract', enforce: 'pre',
    resolveId(id, importer) { if (id.startsWith('\0identity:')) return id; if (importer?.endsWith('/api/auth/index.ts') && mocks[id]) return '\0identity:' + id },
    load(id) { if (id.startsWith('\0identity:')) return mocks[id.slice(10)] },
  }], optimizeDeps: { noDiscovery: true, entries: [] }, server: { middlewareMode: true, hmr: false }, appType: 'custom' })
  ;({ bindVOSIdentity: bind } = await server.ssrLoadModule('/src/api/auth/index.ts'))
  base = await server.ssrLoadModule('\0identity:@/utils/api-base') as typeof base
  Object.defineProperty(globalThis, 'window', { configurable: true, value: { location: { origin: 'https://vos.test' } } })
})

after(async () => {
  await server.close()
  globalThis.fetch = originalFetch
  if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow)
  else Reflect.deleteProperty(globalThis, 'window')
})

test('association pins both proofs to the confirmed session and does not use ambient cookies', async () => {
  let calls = 0
  globalThis.fetch = (async (input, init) => {
    calls++
    assert.equal(input, 'https://vos.test/app/com.ictrek.hybrag/api/v1/auth/vos-identity')
    assert.equal((init?.headers as Record<string, string>).Authorization, 'Bearer confirmed-local-session')
    assert.deepEqual(JSON.parse(String(init?.body)), { access_token: 'fresh-vos-proof' })
    assert.equal(init?.credentials, 'omit')
    assert.equal(init?.redirect, 'error')
    assert.equal(init?.cache, 'no-store')
    return Response.json({ success: true, data: { bound: true } })
  }) as typeof fetch
  assert.deepEqual(await bind('fresh-vos-proof', 'confirmed-local-session'), { success: true })
  assert.equal(calls, 1)
})

test('a conflict is not retried through automatic SSO', async () => {
  let calls = 0
  globalThis.fetch = (async () => { calls++; return Response.json({ code: 'identity_conflict' }, { status: 409 }) }) as typeof fetch
  await assert.rejects(bind('fresh-vos-proof', 'confirmed-local-session'), /identity_conflict/)
  assert.equal(calls, 1)
})

test('an expired confirmed session cannot be replaced by automatic SSO', async () => {
  let calls = 0
  globalThis.fetch = (async () => { calls++; return Response.json({ error: 'unauthorized' }, { status: 401 }) }) as typeof fetch
  await assert.rejects(bind('fresh-vos-proof', 'expired-local-session'), /identity_binding_failed/)
  assert.equal(calls, 1)
})

test('a foreign API origin cannot receive either credential', async () => {
  base.state.value = 'https://foreign.test'
  let calls = 0
  globalThis.fetch = (async () => { calls++; throw new Error('must not fetch') }) as typeof fetch
  try { await assert.rejects(bind('fresh-vos-proof', 'confirmed-local-session'), /invalid_identity_binding_session/) }
  finally { base.state.value = '/app/com.ictrek.hybrag' }
  assert.equal(calls, 0)
})
