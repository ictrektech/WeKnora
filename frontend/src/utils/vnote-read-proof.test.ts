import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { createServer, type ViteDevServer } from 'vite'

let server: ViteDevServer
let headers: typeof import('./vnote-read-proof').getVOSReadHeaders
let bridge: { state: { calls: number; available: boolean; token: string | null } }
let identity = 'session-a'
let writes = 0
const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
const originalStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')

before(async () => {
  const mocks: Record<string, string> = {
    './vos-fastpath': `export const state={calls:0,available:true,token:'human-proof'};export const getVOSFastpathPlatform=()=>state.available?{}:null;export async function acquireVOSFastpathToken(){state.calls++;return state.token?{access_token:state.token,expires_in:300}:null}`,
    './vos-sso': `export const getVOSAccessTokenForIframeSSO=()=>null`,
    './api-base': `export const getApiBaseUrl=()=>'/app/com.ictrek.hybrag'`,
  }
  server = await createServer({ configFile:false, plugins:[{name:'private-read-proof-test',enforce:'pre',
    resolveId(id,importer){if(id.startsWith('\0proof:'))return id;if(importer?.endsWith('/vnote-read-proof.ts')&&mocks[id])return '\0proof:'+id},
    load(id){if(id.startsWith('\0proof:'))return mocks[id.slice(7)]},
  }], optimizeDeps:{noDiscovery:true,entries:[]}, server:{middlewareMode:true,hmr:false}, appType:'custom' })
  ;({getVOSReadHeaders:headers}=await server.ssrLoadModule('/src/utils/vnote-read-proof.ts'))
  bridge=await server.ssrLoadModule('\0proof:./vos-fastpath') as typeof bridge
  Object.defineProperty(globalThis,'window',{configurable:true,value:{location:{origin:'https://vos.test'}}})
  Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:()=>identity,setItem:()=>{writes++}}})
})
after(async()=>{
  await server?.close()
  if(originalWindow)Object.defineProperty(globalThis,'window',originalWindow);else Reflect.deleteProperty(globalThis,'window')
  if(originalStorage)Object.defineProperty(globalThis,'localStorage',originalStorage);else Reflect.deleteProperty(globalThis,'localStorage')
})

test('private proof never goes to another origin, app, or non-API URL',async()=>{
  for(const url of ['https://foreign.test/api/v1/knowledge','//foreign.test/api/v1/knowledge','/app/another/api/v1/knowledge','/download'])assert.deepEqual(await headers(url),{})
  assert.equal(bridge.state.calls,0)
})
test('concurrent API requests share an in-memory proof without storing it',async()=>{
  const results=await Promise.all([headers('/api/v1/knowledge','/app/com.ictrek.hybrag'),headers('/app/com.ictrek.hybrag/api/v1/chat'),headers('https://vos.test/app/com.ictrek.hybrag/api/v1/chunks')])
  assert.ok(results.every(value=>value['X-VOS-User-Proof']==='human-proof'))
  assert.equal(bridge.state.calls,1);assert.equal(writes,0)
})
test('switching the local session reacquires proof and missing proof stays absent',async()=>{
  identity='session-b';await headers('/app/com.ictrek.hybrag/api/v1/knowledge');assert.equal(bridge.state.calls,2)
  identity='session-c';bridge.state.token=null
  assert.deepEqual(await headers('/app/com.ictrek.hybrag/api/v1/knowledge'),{});assert.equal(writes,0)
})
