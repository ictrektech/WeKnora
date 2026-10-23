import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { formatExampleCitations, resolveExampleCitation } from './patentExampleCitations'
import type { PatentExample } from '@/api/patent-examples'

const catalog = JSON.parse(readFileSync(new URL('../../../../internal/patentexamples/data/catalog.json', import.meta.url), 'utf8'))
const examples: PatentExample[] = catalog.examples

test('only reviewed citations within this example become file pills', () => {
  for (const example of examples) {
    for (const message of example.messages || []) {
      const output = formatExampleCitations(message.content, example.documents)
      if (example.workspace_mode === 'patent_writing' || message.role === 'user') {
        assert.equal(output, message.content)
        continue
      }
      assert.match(output, /class="citation citation-example"/)
      assert.doesNotMatch(output, /class="citation citation-kb"|<kb\b|data-chunk-id|data-kb-id/)
      assert.doesNotMatch(output, /\[来源 /)
      for (const href of output.matchAll(/href="([^"]+)"/g)) assert.ok(resolveExampleCitation(href[1]!, example.documents))
    }
  }
})

test('unknown documents, source positions and forged URL forms cannot resolve', () => {
  const documents = examples.find(example => example.id === 'search-political-qa')!.documents
  assert.ok(resolveExampleCitation('#example-source/CN119961390A/abstract', documents))
  for (const href of [
    '#example-source/CN119961396A/abstract', '#example-source/CN119961390A/missing',
    '#example-source/../catalog', '#example-source/CN119961390A/abstract/extra',
    'https://example.test/#example-source/CN119961390A/abstract', '#example-source/CN119961390A/abstract?extra=1',
  ]) assert.equal(resolveExampleCitation(href, documents), null)
})

test('file labels and source locations are escaped before entering markdown HTML', () => {
  const documents = structuredClone(examples.find(example => example.id === 'search-political-qa')!.documents)
  documents[0]!.filename = '"><img src=x onerror=alert(1)>.pdf'
  documents[0]!.publication!.evidence[0]!.location = '<script>unsafe</script>'
  const output = formatExampleCitations('[source](#example-source/CN119961390A/bibliography)', documents)
  assert.doesNotMatch(output, /<img|<script>/)
  assert.match(output, /&lt;img/)
  assert.match(output, /&lt;script&gt;/)
})
