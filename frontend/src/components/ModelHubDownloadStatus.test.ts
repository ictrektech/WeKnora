import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import type { HubModel, HubTask } from '../api/modelHub'

const { descriptor } = parse(readFileSync(new URL('./ModelHubDownloadStatus.vue', import.meta.url), 'utf8'))
const compiled = ts.transpileModule(compileScript(descriptor, { id: 'model-hub-status-test' }).content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

async function fixture(vos = true) {
  let mounted = () => {}, unmounted = () => {}, poll = () => {}
  const listeners = new Map<string, () => void>()
  let reads = 0, pulls = 0, cleared = false
  const hub = { models: [] as HubModel[], tasks: [] as HubTask[], error: false }
  const exports: any = {}
  runInNewContext(compiled, {
    exports,
    window: {
      setInterval(callback: () => void) { poll = callback; return 1 },
      clearInterval() { cleared = true },
      addEventListener(name: string, callback: () => void) { listeners.set(name, callback) },
      removeEventListener(name: string) { listeners.delete(name) },
    },
    require(name: string) {
      if (name === 'vue') return { ...vue, onMounted: (fn: () => void) => { mounted = fn }, onUnmounted: (fn: () => void) => { unmounted = fn } }
      if (name === '@/i18n/vosLocale') return { isVosApp: () => vos }
      if (name === '@/api/modelHub') return {
        listHubModels: async () => { reads++; if (hub.error) throw new Error('Model Hub HTTP 404'); return hub.models },
        listHubTasks: async () => { reads++; if (hub.error) throw new Error('Model Hub HTTP 404'); return hub.tasks },
        pullHubModel: async () => { pulls++; return 'task' },
        retryHubTask: async () => {},
        hubModelId: (name: string) => `ollama://${name.includes(':') ? name : `${name}:latest`}`,
      }
      throw new Error(`Unexpected import: ${name}`)
    },
  })
  const state = exports.default.setup({}, { expose() {} })
  const flush = async () => { await new Promise(resolve => setImmediate(resolve)); await vue.nextTick() }
  return {
    hub, state, listeners,
    reads: () => reads, pulls: () => pulls, cleared: () => cleared,
    mount: async () => { mounted(); await flush() },
    pullEvent: async () => { listeners.get('weknora:model-hub-pull')?.(); await flush() },
    poll: async () => { poll(); await flush() },
    dispose: () => unmounted(),
  }
}

test('VOS with only Scheduler or cloud models never invents Model Hub downloads', async () => {
  for (const error of [true, false]) {
    const f = await fixture()
    f.hub.error = error
    await f.mount()
    await f.poll()
    assert.equal(f.reads(), 0)
    assert.equal(f.state.visible.value, false)
    assert.equal(f.state.pending.value.length, 0)
    assert.equal(f.pulls(), 0)
    f.dispose()
    assert.equal(f.cleared(), false)
    assert.equal(f.listeners.size, 0)
  }
})

test('only actual unfinished Ollama tasks appear, including failures and custom models', async () => {
  const f = await fixture()
  f.hub.models = [{ model_id: 'ollama://installed:latest', status: 'ready' }]
  f.hub.tasks = [
    { task_id: 'custom', model_id: 'ollama://custom:7b', phase: 'DOWNLOADING', progress: 0.4 },
    { task_id: 'failed', model_id: 'ollama://failed:latest', phase: 'FAILED', progress: 0, error_msg: 'Disk full' },
    { task_id: 'ready', model_id: 'ollama://complete:latest', phase: 'READY', progress: 1 },
    { task_id: 'installed', model_id: 'ollama://installed:latest', phase: 'FAILED', progress: 0 },
    { task_id: 'other', model_id: 'other://model', phase: 'PENDING', progress: 0 },
  ]
  await f.mount()
  await f.pullEvent()
  assert.equal(f.state.visible.value, true)
  assert.deepEqual(Array.from(f.state.pending.value, (item: any) => item.name), ['custom:7b', 'failed:latest'])
  assert.equal(f.state.pending.value[0].task.progress, 0.4)
  assert.equal(f.state.pending.value[1].task.error_msg, 'Disk full')
  assert.equal(f.pulls(), 0)

  f.hub.error = true
  await f.poll()
  assert.equal(f.state.visible.value, true)
  assert.equal(f.state.connectionError.value, true)
  assert.equal(f.state.pending.value.length, 2)

  f.hub.error = false
  f.hub.tasks = []
  await f.poll()
  assert.equal(f.state.visible.value, false)
  assert.equal(f.state.connectionError.value, false)
  assert.equal(f.cleared(), true)
  f.dispose()
  assert.equal(f.cleared(), true)
})

test('Model Hub becoming available later is observed without starting downloads', async () => {
  const f = await fixture()
  f.hub.error = true
  await f.mount()
  await f.pullEvent()
  f.hub.error = false
  f.hub.tasks = [{ task_id: 'pending', model_id: 'ollama://new:latest', phase: 'PENDING', progress: 0 }]
  await f.poll()
  assert.equal(f.state.visible.value, true)
  assert.equal(f.state.connectionError.value, false)
  assert.equal(f.pulls(), 0)
  f.dispose()
})

test('standalone deployments do not poll Model Hub', async () => {
  const f = await fixture(false)
  await f.mount()
  await f.poll()
  assert.equal(f.reads(), 0)
  assert.equal(f.state.visible.value, false)
  f.dispose()
})
