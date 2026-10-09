import assert from 'node:assert/strict'
import { test } from 'node:test'

import { browserTraditionalLocale, BUILT_IN_DEFAULT, resolveDefaultLocale } from './resolveDefaultLocale.ts'

test('resolveDefaultLocale prefers runtime over build-time default', () => {
  assert.equal(resolveDefaultLocale('en-US', 'ru-RU'), 'en-US')
})

test('resolveDefaultLocale falls back to build-time then built-in default', () => {
  assert.equal(resolveDefaultLocale('', 'ko-KR'), 'ko-KR')
  assert.equal(resolveDefaultLocale('', 'ja-JP'), 'ja-JP')
  assert.equal(resolveDefaultLocale(undefined, undefined), BUILT_IN_DEFAULT)
})

test('resolveDefaultLocale trims whitespace around supported tags', () => {
  assert.equal(resolveDefaultLocale(' en-US '), 'en-US')
})

test('resolveDefaultLocale rejects unknown values', () => {
  assert.equal(resolveDefaultLocale('fr-FR'), BUILT_IN_DEFAULT)
  assert.equal(resolveDefaultLocale('en-US"};alert(1);//'), BUILT_IN_DEFAULT)
  assert.equal(resolveDefaultLocale('   '), BUILT_IN_DEFAULT)
})

test('resolveDefaultLocale accepts traditional Chinese tags and Hans stays simplified', () => {
  for (const tag of ['zh-TW', 'zh-HK', 'zh-Hant', 'zh-Hant-HK']) {
    assert.equal(resolveDefaultLocale(tag), 'zh-TW')
  }
  for (const tag of ['zh-Hans', 'zh-Hans-TW', 'zh-CN']) {
    assert.equal(resolveDefaultLocale(tag), 'zh-CN')
  }
})

test('browserTraditionalLocale follows the preferred browser tag only', () => {
  assert.equal(browserTraditionalLocale(['zh-TW', 'en-US']), 'zh-TW')
  assert.equal(browserTraditionalLocale(['zh-Hant-HK']), 'zh-TW')
  assert.equal(browserTraditionalLocale(['zh-Hans-TW']), null)
  assert.equal(browserTraditionalLocale(['en-US', 'zh-TW']), null)
})
