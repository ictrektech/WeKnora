export function normalizeReviewText(value: string): string {
  return value.toLowerCase().replace(/[\s\u00a0\u200b\u200c\u200d\ufeff]+/g, '').replace(/[“”‘’]/g, '"')
}

export interface ReviewQuoteMatch {
  start: number
  end: number
}

const omittedTextPattern = /(?:\.{2,}|…+)/

function findOrderedSegments(haystack: string, segments: string[]): ReviewQuoteMatch | null {
  if (!segments.length) return null

  let firstStart = haystack.indexOf(segments[0])
  while (firstStart >= 0) {
    let cursor = firstStart + segments[0].length
    let end = cursor
    let matched = true
    for (const segment of segments.slice(1)) {
      const segmentStart = haystack.indexOf(segment, cursor)
      if (segmentStart < 0) {
        matched = false
        break
      }
      cursor = segmentStart + segment.length
      end = cursor
    }
    if (matched) return { start: firstStart, end }
    firstStart = haystack.indexOf(segments[0], firstStart + 1)
  }
  return null
}

/**
 * Finds a quote in rendered document text and returns offsets in the
 * normalized haystack. AI findings sometimes use an ellipsis to shorten a
 * quote; those ellipses describe omitted text and are not expected to occur
 * literally in the source document.
 */
export function findReviewQuoteMatch(renderedText: string, quote: string): ReviewQuoteMatch | null {
  const haystack = normalizeReviewText(renderedText)
  const needle = normalizeReviewText(quote)
  if (!needle) return null

  const exactStart = haystack.indexOf(needle)
  if (exactStart >= 0) return { start: exactStart, end: exactStart + needle.length }

  const segments = needle.split(omittedTextPattern).filter(Boolean)
  const segmentLength = segments.reduce((total, segment) => total + segment.length, 0)
  if (segments.length > 1 && segmentLength >= 20) {
    const omittedMatch = findOrderedSegments(haystack, segments)
    if (omittedMatch) return omittedMatch
  }

  if (needle.length < 40) return null
  const samples = Array.from(new Set([
    needle.slice(0, 40),
    needle.slice(Math.max(0, Math.floor(needle.length / 2) - 20), Math.floor(needle.length / 2) + 20),
    needle.slice(-40),
  ]))
  const sampleMatches = samples
    .map((sample) => {
      const start = haystack.indexOf(sample)
      return start >= 0 ? { start, end: start + sample.length } : null
    })
    .filter((match): match is ReviewQuoteMatch => match !== null)
  if (sampleMatches.length < 2) return null
  return {
    start: Math.min(...sampleMatches.map((match) => match.start)),
    end: Math.max(...sampleMatches.map((match) => match.end)),
  }
}

export function hasReviewQuoteMatch(renderedText: string, quote: string): boolean {
  return findReviewQuoteMatch(renderedText, quote) !== null
}
