import assert from 'node:assert/strict'
import { test, type TestContext } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { followsVosLocale, initialAppLocale, listenForVosLocaleChange, readVosLocale, saveAppLocale } from './vosLocale.ts'

function environment(t: TestContext, embedded = true) {
  const values = new Map<string, string>()
  const listeners = new Map<string, (event: StorageEvent) => void>()
  const storage = {
    get length() { return values.size },
    key(index: number) { return [...values.keys()][index] ?? null },
    getItem(key: string) { return values.get(key) ?? null },
    setItem(key: string, value: string) { values.set(key, value) },
    removeItem(key: string) { values.delete(key) },
  }
  const win = {
    location: { pathname: embedded ? '/app/com.ictrek.hybrag/' : '/' },
    parent: {} as unknown,
    vos_platform: undefined as Window['vos_platform'],
    addEventListener: (name: string, listener: (event: StorageEvent) => void) => listeners.set(name, listener),
    removeEventListener: (name: string) => listeners.delete(name),
  }
  if (!embedded) win.parent = win
  const restores: (() => void)[] = []
  const stops: (() => void)[] = []
  t.after(() => {
    stops.forEach((stop) => stop())
    restores.forEach((restore) => restore())
  })
  for (const [key, value] of Object.entries({
    window: win, localStorage: storage, navigator: { languages: ['en-US'], language: 'en-US' },
  })) {
    const old = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value })
    restores.push(() => {
      if (old) Object.defineProperty(globalThis, key, old)
      else Reflect.deleteProperty(globalThis, key)
    })
  }
  return {
    win, storage, listeners,
    listen(apply: Parameters<typeof listenForVosLocaleChange>[0]) {
      const stop = listenForVosLocaleChange(apply)
      stops.push(stop)
      return stop
    },
  }
}

function bridge(getState = async (): Promise<{ locale?: unknown }> => ({ locale: 'zh-CN' })) {
  let handler: ((data: { locale?: unknown }) => void) | undefined
  const order: string[] = []
  return {
    order,
    emit(locale: unknown) { handler?.({ locale }) },
    getState() { order.push('snapshot'); return getState() },
    broadcast: {
      on(event: 'locale-change', callback: (data: { locale?: unknown }) => void) {
        assert.equal(event, 'locale-change')
        order.push('subscribe')
        handler = callback
        return () => { handler = undefined; order.push('unsubscribe') }
      },
    },
  }
}

test('automatic follows legacy VOS storage; manual choice persists', (t) => {
  const { storage, listeners, listen } = environment(t)
  const key = 'VIVIBIT-5.5.9-prod-preferences-locale'
  storage.setItem(key, JSON.stringify({ value: 'zh-CN' }))
  assert.equal(readVosLocale(), 'zh-CN')
  assert.equal(initialAppLocale('en-US'), 'zh-CN')
  assert.equal(followsVosLocale(), true)
  let applied = ''
  listen((locale) => { applied = locale })
  listeners.get('storage')?.({ key, newValue: JSON.stringify({ value: 'ja-JP' }) } as StorageEvent)
  assert.equal(applied, 'ja-JP')
  assert.equal(saveAppLocale('ko-KR'), 'ko-KR')
  listeners.get('storage')?.({ key, newValue: JSON.stringify({ value: 'ru-RU' }) } as StorageEvent)
  assert.equal(applied, 'ja-JP')
  assert.equal(initialAppLocale('en-US'), 'ko-KR')
  assert.equal(saveAppLocale('auto'), 'zh-CN')
  assert.equal(storage.getItem('locale'), null)
})

test('subscribes before snapshot; maps events and caches VOS changes during manual mode', async (t) => {
  const { win, storage, listeners, listen } = environment(t)
  const platform = bridge()
  win.vos_platform = platform
  let applied = ''
  const stop = listen((locale) => { applied = locale })
  await setImmediate()
  assert.deepEqual(platform.order, ['subscribe', 'snapshot'])
  assert.equal(applied, 'zh-CN', 'VOS wins over an English browser')
  platform.emit('en')
  assert.equal(applied, 'en-US')
  platform.emit('zh-Hant')
  assert.equal(applied, 'zh-TW')
  platform.emit('fr-FR')
  platform.emit(null)
  assert.equal(applied, 'zh-TW')
  storage.setItem('preferences-locale', JSON.stringify({ value: 'ru-RU' }))
  listeners.get('storage')?.({ key: 'preferences-locale', newValue: storage.getItem('preferences-locale') } as StorageEvent)
  assert.equal(applied, 'zh-TW', 'legacy storage cannot override a connected bridge')
  saveAppLocale('ja-JP')
  platform.emit('zh-CN')
  assert.equal(applied, 'zh-TW', 'event must not override manual selection')
  assert.equal(initialAppLocale('en-US'), 'ja-JP')
  assert.equal(saveAppLocale('auto'), 'zh-CN', 'auto uses the latest cached VOS locale')
  platform.emit('en-US')
  assert.equal(applied, 'en-US')
  stop()
  assert.equal(platform.order.at(-1), 'unsubscribe')
  platform.emit('zh-CN')
  assert.equal(applied, 'en-US')
})

test('a newer event prevents a slow snapshot from overwriting it', async (t) => {
  const { win, listen } = environment(t)
  let resolve!: (state: { locale: string }) => void
  const platform = bridge(() => new Promise((done) => { resolve = done }))
  win.vos_platform = platform
  let applied = ''
  listen((locale) => { applied = locale })
  await setImmediate()
  platform.emit('en-US')
  resolve({ locale: 'zh-CN' })
  await setImmediate()
  assert.equal(applied, 'en-US')
  assert.equal(saveAppLocale('auto'), 'en-US')
})

test('waits for delayed bridge injection and cancels polling on cleanup', async (t) => {
  const { win, listen } = environment(t)
  t.mock.timers.enable({ apis: ['setInterval'] })
  let applied = ''
  const stop = listen((locale) => { applied = locale })
  assert.equal(initialAppLocale('zh-CN'), 'en-US')
  t.mock.timers.tick(5000)
  const platform = bridge()
  win.vos_platform = platform
  t.mock.timers.tick(250)
  await setImmediate()
  assert.equal(applied, 'zh-CN')
  t.mock.timers.tick(1000)
  assert.deepEqual(platform.order, ['subscribe', 'snapshot'])
  stop()
  platform.emit('en-US')
  assert.equal(applied, 'zh-CN')
})

test('snapshot failure keeps browser fallback and later events still work', async (t) => {
  const { win, listen } = environment(t)
  const platform = bridge(async () => { throw new Error('bridge unavailable') })
  win.vos_platform = platform
  let applied = initialAppLocale('zh-CN')
  listen((locale) => { applied = locale })
  await setImmediate()
  assert.equal(applied, 'en-US')
  platform.emit('zh-CN')
  assert.equal(applied, 'zh-CN')
})

test('cleanup ignores an outstanding snapshot and cancels injection waiting', async (t) => {
  const { win, listen } = environment(t)
  t.mock.timers.enable({ apis: ['setInterval'] })
  let applied = ''
  const stopWaiting = listen((locale) => { applied = locale })
  stopWaiting()
  let resolve!: (state: { locale: string }) => void
  const platform = bridge(() => new Promise((done) => { resolve = done }))
  win.vos_platform = platform
  t.mock.timers.tick(250)
  assert.deepEqual(platform.order, [])
  const stop = listen((locale) => { applied = locale })
  await setImmediate()
  stop()
  resolve({ locale: 'zh-CN' })
  await setImmediate()
  assert.equal(applied, '')
})

test('standalone ignores VOS storage and follows browser then deployment default', (t) => {
  const { storage, win, listen } = environment(t, false)
  storage.setItem('preferences-locale', JSON.stringify({ value: 'zh-CN' }))
  win.vos_platform = bridge()
  listen(() => assert.fail('standalone must not receive VOS updates'))
  assert.equal(followsVosLocale(), false)
  assert.equal(readVosLocale(), null)
  assert.equal(initialAppLocale('zh-CN'), 'en-US')
  assert.equal(saveAppLocale('ja-JP'), 'ja-JP')
  assert.equal(saveAppLocale('auto'), 'en-US')
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { languages: ['fr-FR'] } })
  assert.equal(initialAppLocale('ko-KR'), 'ko-KR')
})
