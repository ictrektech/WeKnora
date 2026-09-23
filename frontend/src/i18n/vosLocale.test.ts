import assert from 'node:assert/strict'
import { test } from 'node:test'

import { initialAppLocale, listenForVosLocaleChange, readVosLocale, saveAppLocale } from './vosLocale.ts'

test('follows the namespaced VOS locale and its storage changes', () => {
  const oldWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
  const oldStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  const values = new Map<string, string>()
  const listeners = new Map<string, (event: StorageEvent) => void>()
  const storage = {
    get length() { return values.size },
    key(index: number) { return [...values.keys()][index] ?? null },
    getItem(key: string) { return values.get(key) ?? null },
    setItem(key: string, value: string) { values.set(key, value) },
    removeItem(key: string) { values.delete(key) },
  }
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      location: { pathname: '/app/com.ictrek.hybrag/' },
      addEventListener: (name: string, listener: (event: StorageEvent) => void) => listeners.set(name, listener),
      removeEventListener: (name: string) => listeners.delete(name),
    },
  })
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage })

  try {
    const key = 'vben-web-antd-1.1.0-prod-preferences-locale'
    storage.setItem(key, JSON.stringify({ value: 'en-US' }))
    assert.equal(readVosLocale(), 'en-US')
    assert.equal(initialAppLocale('zh-CN'), 'en-US')

    let applied = ''
    const stop = listenForVosLocaleChange((locale) => { applied = locale })
    listeners.get('storage')?.({ key, newValue: JSON.stringify({ value: 'ja-JP' }) } as StorageEvent)
    assert.equal(applied, 'ja-JP')

    saveAppLocale('ko-KR')
    listeners.get('storage')?.({ key, newValue: JSON.stringify({ value: 'ru-RU' }) } as StorageEvent)
    assert.equal(applied, 'ja-JP')
    stop()
  } finally {
    if (oldWindow) Object.defineProperty(globalThis, 'window', oldWindow)
    else Reflect.deleteProperty(globalThis, 'window')
    if (oldStorage) Object.defineProperty(globalThis, 'localStorage', oldStorage)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})
