import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { test } from 'node:test'
import { localizedApiDocs } from './apiDocs.ts'

const modules: Record<string, string> = {}
for (const directory of ['website-docs/04-api', 'docs/api']) {
  const url = new URL(`../../../${directory}/`, import.meta.url)
  for (const name of readdirSync(url).filter((name) => name.endsWith('.md'))) {
    modules[`../../../../${directory}/${name}`] = readFileSync(new URL(name, url), 'utf8')
  }
}

test('every bundled Chinese chapter has a full English counterpart with the same API endpoints', () => {
  const chinese = localizedApiDocs(modules, 'zh-CN')
  const english = localizedApiDocs(modules, 'en-US')
  assert.ok(chinese.length >= 19)
  assert.deepEqual(english.map((d) => d.slug), chinese.map((d) => d.slug))
  const endpoints = (text: string) => [...text.matchAll(/^#{2,6}\s+((?:GET|POST|PUT|PATCH|DELETE|HEAD)(?:\s*\/\s*(?:GET|POST|PUT|PATCH|DELETE|HEAD))*\s+[^\s（]+)/gm)].map((m) => m[1])
  for (let i = 0; i < chinese.length; i++) {
    assert.deepEqual(endpoints(english[i].raw), endpoints(chinese[i].raw), chinese[i].slug)
    assert.ok(!/[\u3400-\u9fff]/u.test(english[i].title), english[i].title)
    assert.equal((english[i].raw.match(/^```/gm) || []).length, (chinese[i].raw.match(/^```/gm) || []).length, chinese[i].slug)
  }
  assert.deepEqual(localizedApiDocs(modules, 'ja-JP'), english)
  assert.deepEqual(localizedApiDocs(modules, 'zh-TW'), chinese)
  assert.equal(english[0].slug, 'vos-external-api')
})
