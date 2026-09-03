/**
 * Pure helpers shared by the contract-review panel and document viewer.
 *
 * Evidence matching is deliberately conservative. The rendered document is
 * searched as one normalized text stream, every exact candidate is returned,
 * and callers decide whether a single candidate is safe to highlight. There
 * is no fuzzy, ellipsis, or first-hit matching here.
 */

export type EvidenceStatus = 'pending' | 'located' | 'legacy_exact' | 'multiple_matches' | 'not_found' | 'version_mismatch' | 'unsupported' | 'error'
export type LocatorLoadStatus = 'idle' | 'loading' | 'ready' | 'unsupported' | 'error'

export interface ReviewQuoteMatch {
  start: number
  end: number
}

export interface NormalizedTextMap {
  text: string
  offsets: Array<{ start: number; end: number }>
}

export interface ReviewLocatorUnitLike {
  unit_id: string
  source_start: number
  source_end: number
  page?: number
  rendered_start?: number
  rendered_end?: number
}

export interface ReviewLocatorLike {
  source_revision?: string
  source_hash?: string
  source_text_hash?: string
  text?: string
  source_text?: string
  units?: ReviewLocatorUnitLike[]
}

export interface RenderedUnitRange {
  unitId: string
  sourceStart: number
  sourceEnd: number
  renderedStart: number
  renderedEnd: number
  page?: number
}

export interface RenderedPageRange {
  page: number
  renderedStart: number
  renderedEnd: number
}

export interface EvidenceResolutionInput {
  renderedText: string
  quote: string
  sourceStart?: number
  sourceEnd?: number
  expectedRevision?: string
  evidenceRevision?: string
  locator?: ReviewLocatorLike | null
  locatorStatus?: LocatorLoadStatus
  renderedUnitRanges?: RenderedUnitRange[]
  renderedPageRanges?: RenderedPageRange[]
}

export interface EvidenceResolution {
  status: EvidenceStatus
  matches: ReviewQuoteMatch[]
  unitIds: string[]
  pageStart?: number
  pageEnd?: number
  reason?: string
}

const LAYOUT_WHITESPACE = /[\s\u00a0\u1680\u180e\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\u200b\u200c\u200d\ufeff]+/g

/** Normalize only layout whitespace; punctuation, case, and wording remain exact. */
export function normalizeReviewText(value: string): string {
	return value.replace(LAYOUT_WHITESPACE, '')
}

/** Keep a normalized character-to-raw offset map for DOM Range creation. */
export function buildNormalizedTextMap(value: string): NormalizedTextMap {
  let text = ''
  const offsets: Array<{ start: number; end: number }> = []
  for (let index = 0; index < value.length;) {
    const codePoint = value.codePointAt(index)
    if (codePoint === undefined) break
    const rawChar = String.fromCodePoint(codePoint)
    const normalizedChar = normalizeReviewText(rawChar)
    if (normalizedChar) {
      text += normalizedChar
      for (let offset = 0; offset < normalizedChar.length; offset++) offsets.push({ start: index, end: index + rawChar.length })
    }
    index += rawChar.length
  }
  return { text, offsets }
}

/** Return every exact match in normalized offsets, including duplicate text. */
export function findReviewQuoteMatches(renderedText: string, quote: string): ReviewQuoteMatch[] {
  const haystack = normalizeReviewText(renderedText)
  const needle = normalizeReviewText(quote)
  if (!needle) return []

  const matches: ReviewQuoteMatch[] = []
  let start = haystack.indexOf(needle)
  while (start >= 0) {
    matches.push({ start, end: start + needle.length })
    // Advancing by one also makes the helper correct for overlapping exact
    // matches. A caller can still reject ambiguity rather than guessing.
    start = haystack.indexOf(needle, start + 1)
  }
  return matches
}

/** Compatibility helper: return a location only when it is unambiguous. */
export function findReviewQuoteMatch(renderedText: string, quote: string): ReviewQuoteMatch | null {
  const matches = findReviewQuoteMatches(renderedText, quote)
  return matches.length === 1 ? matches[0] : null
}

export function hasReviewQuoteMatch(renderedText: string, quote: string): boolean {
  return findReviewQuoteMatches(renderedText, quote).length > 0
}

function sliceByCodePoints(value: string, start: number, end: number): string {
  return Array.from(value).slice(start, end).join('')
}

function validSourceRange(sourceStart?: number, sourceEnd?: number): sourceStart is number {
  return sourceStart !== undefined && sourceEnd !== undefined && Number.isInteger(sourceStart) && Number.isInteger(sourceEnd) && sourceStart >= 0 && sourceEnd > sourceStart
}

function sourceUnitsForRange(locator: ReviewLocatorLike, sourceStart: number, sourceEnd: number): ReviewLocatorUnitLike[] {
  return (locator.units || []).filter((unit) => {
    return Number.isFinite(unit.source_start)
      && Number.isFinite(unit.source_end)
      && unit.source_start < sourceEnd
      && unit.source_end > sourceStart
  })
}

function finishResolution(
  matches: ReviewQuoteMatch[],
  unitIds: string[],
  pages: number[],
  statusForSingle: EvidenceStatus,
  reason?: string,
): EvidenceResolution {
  if (!matches.length) return { status: 'not_found', matches: [], unitIds, reason }
  if (matches.length > 1) return {
    status: 'multiple_matches',
    matches,
    unitIds,
    ...(pages.length ? { pageStart: Math.min(...pages), pageEnd: Math.max(...pages) } : {}),
    reason,
  }
  return {
    status: statusForSingle,
    matches,
    unitIds,
    ...(pages.length ? { pageStart: Math.min(...pages), pageEnd: Math.max(...pages) } : {}),
    reason,
  }
}

/**
 * Resolve an issue against one rendered document revision.
 *
 * When a locator is available, source offsets and revision checks are
 * authoritative. Optional rendered unit offsets can disambiguate repeated
 * phrases. Without those offsets, duplicate exact matches stay visibly
 * ambiguous. When an older server has no locator endpoint, the result remains
 * explicitly unsupported; a text-only match is never promoted to evidence.
 */
export function resolveReviewEvidence(input: EvidenceResolutionInput): EvidenceResolution {
  const {
    renderedText,
    quote,
    sourceStart,
    sourceEnd,
    expectedRevision,
    evidenceRevision,
    locator,
    locatorStatus = 'idle',
    renderedUnitRanges = [],
    renderedPageRanges = [],
  } = input

  if (!normalizeReviewText(quote)) return { status: 'not_found', matches: [], unitIds: [], reason: 'empty-quote' }
  if (expectedRevision && evidenceRevision && expectedRevision !== evidenceRevision) {
    return { status: 'version_mismatch', matches: [], unitIds: [], reason: 'evidence-revision-mismatch' }
  }
  if (locatorStatus === 'idle' || locatorStatus === 'loading') {
    return { status: 'pending', matches: [], unitIds: [], reason: 'locator-loading' }
  }
  if (locatorStatus === 'error') {
    return { status: 'error', matches: [], unitIds: [], reason: 'locator-error' }
  }

	const exactMatches = findReviewQuoteMatches(renderedText, quote)
	if (locatorStatus === 'unsupported' || !locator) {
		return { status: 'unsupported', matches: exactMatches, unitIds: [], reason: 'locator-unsupported' }
  }

  if (locator.source_revision && expectedRevision && locator.source_revision !== expectedRevision) {
    return { status: 'version_mismatch', matches: [], unitIds: [], reason: 'locator-revision-mismatch' }
  }
  if (!validSourceRange(sourceStart, sourceEnd)) {
    return { status: 'unsupported', matches: [], unitIds: [], reason: 'source-range-missing' }
  }

  const resolvedSourceStart = sourceStart as number
  const resolvedSourceEnd = sourceEnd as number
  const units = sourceUnitsForRange(locator, resolvedSourceStart, resolvedSourceEnd)
  const unitIds = units.map((unit) => unit.unit_id)
  const pages = units.flatMap((unit) => Number.isFinite(unit.page) ? [unit.page as number] : [])
  const sourceText = locator.source_text ?? locator.text
  if (sourceText !== undefined) {
    const sourceQuote = sliceByCodePoints(sourceText, resolvedSourceStart, resolvedSourceEnd)
    if (normalizeReviewText(sourceQuote) !== normalizeReviewText(quote)) {
      return { status: 'not_found', matches: [], unitIds, pageStart: pages.length ? Math.min(...pages) : undefined, pageEnd: pages.length ? Math.max(...pages) : undefined, reason: 'source-quote-mismatch' }
    }
  }
  if (!units.length) {
    return { status: 'not_found', matches: [], unitIds: [], reason: 'source-units-missing' }
  }

  const unitRanges = renderedUnitRanges.filter((range) => {
    return range.sourceStart < resolvedSourceEnd && range.sourceEnd > resolvedSourceStart
  })
  let candidateMatches = exactMatches
  if (unitRanges.length) {
    candidateMatches = exactMatches.filter((match) => unitRanges.some((range) => {
      return match.start < range.renderedEnd && match.end > range.renderedStart
    }))
  }
  const selectedPages = new Set(pages)
  if (selectedPages.size && renderedPageRanges.length) {
    candidateMatches = candidateMatches.filter((match) => {
      const matchPages = renderedPageRanges
        .filter((range) => match.start < range.renderedEnd && match.end > range.renderedStart)
        .map((range) => range.page)
      return matchPages.length > 0 && matchPages.every((page) => selectedPages.has(page))
    })
  }
  return finishResolution(candidateMatches, unitIds, pages, 'located', unitRanges.length ? 'source-range-filtered' : undefined)
}

export type ReviewCollectionState = 'not_loaded' | 'empty' | 'populated'

/** Distinguish an omitted streaming field from a completed empty result. */
export function reviewCollectionState<T>(items: T[] | undefined): ReviewCollectionState {
  if (!Array.isArray(items)) return 'not_loaded'
  return items.length ? 'populated' : 'empty'
}
