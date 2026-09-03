import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  findReviewQuoteMatch,
  findReviewQuoteMatches,
  hasReviewQuoteMatch,
  normalizeReviewText,
  resolveReviewEvidence,
  reviewCollectionState,
} from './documentLinking.ts'

test('normalizes layout whitespace while preserving case and punctuation', () => {
  assert.equal(normalizeReviewText(' “Payment”\n terms '), '“Payment”terms')
  assert.equal(normalizeReviewText('payment\u00a0terms'), 'paymentterms')
  assert.notEqual(normalizeReviewText('Payment'), normalizeReviewText('payment'))
  assert.notEqual(normalizeReviewText('“Payment”'), normalizeReviewText('"Payment"'))
})

test('matches an exact quote despite PDF text-item whitespace', () => {
  assert.equal(hasReviewQuoteMatch('Payment shall be made within 30 days.', 'Payment shall be made\nwithin 30 days.'), true)
  assert.deepEqual(findReviewQuoteMatches('Payment shall be made within 30 days.', 'Payment shall be made\nwithin 30 days.'), [{ start: 0, end: 31 }])
})

test('returns every duplicate exact candidate and the compatibility helper refuses to choose', () => {
  const rendered = 'First. Payment is due. Second. Payment is due.'
  const matches = findReviewQuoteMatches(rendered, 'Payment is due')
  assert.equal(matches.length, 2)
  assert.equal(findReviewQuoteMatch(rendered, 'Payment is due'), null)
})

test('does not accept ellipsis or fuzzy samples as exact evidence', () => {
  const rendered = '5.4.1 乙方应与第三方交涉，并承担可能发生的一切法律责任、费用和后果。'
  assert.equal(hasReviewQuoteMatch(rendered, '5.4.1...乙方应与第三方交涉，并承担可能发生的一切法律责任、费用和后果...'), false)
  assert.equal(hasReviewQuoteMatch('A'.repeat(40) + 'X'.repeat(60) + 'B'.repeat(40), 'A'.repeat(40) + 'B'.repeat(40) + 'C'.repeat(40)), false)
})

test('resolves a unique source-range match and preserves Unicode code-point offsets', () => {
  const source = '甲方应在 30 日内付款。'
  const start = Array.from(source).indexOf('3')
  const end = Array.from(source).length
  const result = resolveReviewEvidence({
    renderedText: '甲方应在30日内付款。',
    quote: '30 日内付款。',
    sourceStart: start,
    sourceEnd: end,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      source_text: source,
      units: [{ unit_id: 'p1', source_start: 0, source_end: end, page: 1 }],
    },
  })
  assert.equal(result.status, 'located')
  assert.deepEqual(result.unitIds, ['p1'])
  assert.equal(result.pageStart, 1)
})

test('uses rendered unit offsets to disambiguate repeated source text', () => {
  const result = resolveReviewEvidence({
    renderedText: '付款日为30日。付款日为30日。',
    quote: '付款日为30日。',
    sourceStart: 0,
    sourceEnd: 8,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: { source_revision: 'rev-1', units: [{ unit_id: 'p1', source_start: 0, source_end: 8, page: 1 }] },
    renderedUnitRanges: [{ unitId: 'p1', sourceStart: 0, sourceEnd: 8, renderedStart: 0, renderedEnd: 8, page: 1 }],
  })
  assert.equal(result.status, 'located')
  assert.equal(result.matches.length, 1)
})

test('reports duplicate, missing, and stale evidence instead of highlighting a guess', () => {
  const base = {
    sourceStart: 0,
    sourceEnd: 5,
    locatorStatus: 'ready' as const,
    locator: { source_revision: 'rev-1', units: [{ unit_id: 'p1', source_start: 0, source_end: 5, page: 1 }] },
  }
  assert.equal(resolveReviewEvidence({ ...base, renderedText: 'alpha alpha', quote: 'alpha', expectedRevision: 'rev-1', evidenceRevision: 'rev-1' }).status, 'multiple_matches')
  assert.equal(resolveReviewEvidence({ ...base, renderedText: 'beta', quote: 'alpha', expectedRevision: 'rev-1', evidenceRevision: 'rev-1' }).status, 'not_found')
  assert.equal(resolveReviewEvidence({ ...base, renderedText: 'alpha', quote: 'alpha', expectedRevision: 'rev-2', evidenceRevision: 'rev-1' }).status, 'version_mismatch')
})

test('legacy servers remain explicitly unsupported even when text matches', () => {
  assert.equal(resolveReviewEvidence({ renderedText: 'alpha', quote: 'alpha', sourceStart: 0, sourceEnd: 5, locatorStatus: 'unsupported' }).status, 'unsupported')
  assert.equal(resolveReviewEvidence({ renderedText: 'alpha alpha', quote: 'alpha', locatorStatus: 'unsupported' }).status, 'unsupported')
})

test('distinguishes omitted issue data from a loaded empty result', () => {
  assert.equal(reviewCollectionState(undefined), 'not_loaded')
  assert.equal(reviewCollectionState([]), 'empty')
  assert.equal(reviewCollectionState([{ id: 'issue-1' }]), 'populated')
})
