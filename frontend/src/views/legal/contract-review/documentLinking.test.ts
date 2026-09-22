import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  deduplicatePdfTextItems,
  findReviewQuoteMatch,
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
  assert.deepEqual(findReviewQuoteMatch('Payment shall be made within 30 days.', 'Payment shall be made\nwithin 30 days.'), { start: 0, end: 31 })
})

test('collapses overlaid PDF text items but keeps spatially separate repeats', () => {
  const item = (str: string, x: number) => ({ str, transform: [1, 0, 0, 1, x, 420], width: 96, height: 12 })
  const items = [
    item('合计金额', 50.106),
    item('合计金额', 49.806),
    item('人民币（大写）', 100.106),
    item('人民币（大写）', 99.806),
    item('：', 200.106),
    item('：', 199.806),
    item('合计金额', 180),
  ]
  const quote = '合计金额人民币（大写）：'
  const originalText = items.map((entry) => entry.str).join('')
  const unique = deduplicatePdfTextItems(items)
  const deduplicatedText = unique.map((entry) => entry.str).join('')

  assert.equal(findReviewQuoteMatch(originalText, quote), null)
  assert.deepEqual(findReviewQuoteMatch(deduplicatedText, quote), { start: 0, end: normalizeReviewText(quote).length })
  assert.equal(findReviewQuoteMatch(deduplicatedText, '合计金额'), null)
  assert.equal(unique.length, 4)
  assert.deepEqual(unique.map((entry) => [entry.str, entry.transform[4]]), [
    ['合计金额', 50.106],
    ['人民币（大写）', 100.106],
    ['：', 200.106],
    ['合计金额', 180],
  ])
  assert.equal(deduplicatePdfTextItems([item('甲方', 50), { ...item('甲方', 50), transform: [0, 1, -1, 0, 50, 420] }]).length, 2)
})

test('does not choose between duplicate exact matches without a locator constraint', () => {
  const rendered = 'First. Payment is due. Second. Payment is due.'
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

test('uses the source-relative offset to locate the second duplicate in one unit', () => {
  const source = '付款日为30日。付款日为30日。'
  const quote = '付款日为30日。'
  const sourceStart = Array.from(source).indexOf('付', 1)
  const sourceEnd = sourceStart + Array.from(quote).length
  const rendered = source
  const normalizedPrefix = normalizeReviewText(Array.from(source).slice(0, sourceStart).join('')).length
  const normalizedQuote = normalizeReviewText(quote).length
  const result = resolveReviewEvidence({
    renderedText: rendered,
    quote,
    sourceStart,
    sourceEnd,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      units: [{ unit_id: 'p1', source_start: 0, source_end: Array.from(source).length, text: source, page: 1 }],
    },
  })
  assert.equal(result.status, 'located')
  assert.deepEqual(result.matches, [{ start: normalizedPrefix, end: normalizedPrefix + normalizedQuote }])
})

test('uses a unique source unit to locate repeated wording in different units', () => {
  const firstUnit = 'First payment is due.'
  const secondUnit = 'Second payment is due.'
  const source = `${firstUnit}\n\n${secondUnit}`
  const quote = 'payment is due.'
  const secondUnitStart = Array.from(firstUnit).length + 2
  const sourceStart = secondUnitStart + Array.from('Second ').length
  const sourceEnd = sourceStart + Array.from(quote).length
  const rendered = `${firstUnit}${secondUnit}`
  const normalizedStart = normalizeReviewText('First payment is due.Second ').length
  const result = resolveReviewEvidence({
    renderedText: rendered,
    quote,
    sourceStart,
    sourceEnd,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      units: [
        { unit_id: 'p1', source_start: 0, source_end: Array.from(firstUnit).length, text: firstUnit, page: 1 },
        { unit_id: 'p2', source_start: secondUnitStart, source_end: Array.from(source).length, text: secondUnit, page: 1 },
      ],
    },
  })
  assert.equal(result.status, 'located')
  assert.deepEqual(result.matches, [{ start: normalizedStart, end: normalizedStart + normalizeReviewText(quote).length }])
})

test('keeps identical source units ambiguous when page does not disambiguate them', () => {
  const unitText = 'Payment is due.'
  const source = `${unitText}\n\n${unitText}`
  const secondStart = Array.from(unitText).length + 2
  const result = resolveReviewEvidence({
    renderedText: `${unitText}${unitText}`,
    quote: unitText,
    sourceStart: secondStart,
    sourceEnd: secondStart + Array.from(unitText).length,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      units: [
        { unit_id: 'p1', source_start: 0, source_end: Array.from(unitText).length, text: unitText, page: 1 },
        { unit_id: 'p2', source_start: secondStart, source_end: Array.from(source).length, text: unitText, page: 1 },
      ],
    },
  })
  assert.equal(result.status, 'multiple_matches')
  assert.equal(result.matches.length, 2)
})

test('uses the referenced page to disambiguate identical source units', () => {
  const unitText = 'Payment is due.'
  const source = `${unitText}\n\n${unitText}`
  const secondStart = Array.from(unitText).length + 2
  const normalizedUnitLength = normalizeReviewText(unitText).length
  const result = resolveReviewEvidence({
    renderedText: `${unitText}${unitText}`,
    quote: unitText,
    sourceStart: secondStart,
    sourceEnd: secondStart + Array.from(unitText).length,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      units: [
        { unit_id: 'p1', source_start: 0, source_end: Array.from(unitText).length, text: unitText, page: 1 },
        { unit_id: 'p2', source_start: secondStart, source_end: Array.from(source).length, text: unitText, page: 2 },
      ],
    },
    renderedPageRanges: [
      { page: 1, renderedStart: 0, renderedEnd: normalizedUnitLength },
      { page: 2, renderedStart: normalizedUnitLength, renderedEnd: normalizedUnitLength * 2 },
    ],
  })
  assert.equal(result.status, 'located')
  assert.deepEqual(result.matches, [{ start: normalizedUnitLength, end: normalizedUnitLength * 2 }])
})

test('uses a unique page match when the full source unit cannot be aligned', () => {
  const sourceUnit = 'Payment is due. The due date is fixed.'
  const quote = 'Payment is due.'
  const sourceStart = 0
  const sourceEnd = Array.from(quote).length
  const normalizedQuoteLength = normalizeReviewText(quote).length
  const result = resolveReviewEvidence({
    renderedText: 'Unrelated page text.Payment is due.',
    quote,
    sourceStart,
    sourceEnd,
    expectedRevision: 'rev-1',
    evidenceRevision: 'rev-1',
    locatorStatus: 'ready',
    locator: {
      source_revision: 'rev-1',
      units: [{ unit_id: 'p2', source_start: 0, source_end: Array.from(sourceUnit).length, text: sourceUnit, page: 2 }],
    },
    renderedPageRanges: [
      { page: 1, renderedStart: 0, renderedEnd: normalizeReviewText('Unrelated page text.').length },
      { page: 2, renderedStart: normalizeReviewText('Unrelated page text.').length, renderedEnd: normalizeReviewText('Unrelated page text.Payment is due.').length },
    ],
  })
  assert.equal(result.status, 'located')
  assert.deepEqual(result.matches, [{ start: normalizeReviewText('Unrelated page text.').length, end: normalizeReviewText('Unrelated page text.').length + normalizedQuoteLength }])
})

test('resolves duplicate, missing, and stale evidence without guessing', () => {
  const base = {
    sourceStart: 0,
    sourceEnd: 5,
    locatorStatus: 'ready' as const,
    locator: { source_revision: 'rev-1', units: [{ unit_id: 'p1', source_start: 0, source_end: 5, page: 1 }] },
  }
  const duplicate = resolveReviewEvidence({ ...base, renderedText: 'alpha alpha', quote: 'alpha', expectedRevision: 'rev-1', evidenceRevision: 'rev-1' })
  assert.equal(duplicate.status, 'multiple_matches')
  assert.deepEqual(duplicate.matches, [{ start: 0, end: 5 }, { start: 5, end: 10 }])
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
