import assert from 'node:assert/strict'
import { test } from 'node:test'
import { findReviewQuoteMatch, hasReviewQuoteMatch, normalizeReviewText } from './documentLinking.ts'

test('normalizes whitespace and smart quotes for viewer linking', () => {
  assert.equal(normalizeReviewText(' “Payment”\n terms '), '"payment"terms')
})

test('matches an exact quote despite PDF text-item whitespace', () => {
  assert.equal(hasReviewQuoteMatch('Payment shall be made within 30 days.', 'Payment shall be made\nwithin 30 days.'), true)
})

test('matches a quote that uses ellipses to omit text from the source', () => {
  const rendered = '5.4.1 乙方应与第三方交涉，并承担可能发生的一切法律责任、费用和后果。'
  const quote = '5.4.1...乙方应与第三方交涉，并承担可能发生的一切法律责任、费用和后果...'
  const match = findReviewQuoteMatch(rendered, quote)
  assert.ok(match)
  assert.equal(match.start, 0)
  assert.equal(match.end, normalizeReviewText(rendered).indexOf('。'))
})

test('does not accept a short unrelated quote as a fuzzy match', () => {
  assert.equal(hasReviewQuoteMatch('Confidentiality survives termination.', 'Payment is due'), false)
})
