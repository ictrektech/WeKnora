/**
 * Pure helpers shared by the contract-review panel and document viewer.
 *
 * Evidence matching is deliberately conservative. The rendered document is
 * searched as one normalized text stream, exact matches are constrained by
 * the source locator when available, and ambiguous matches are never guessed.
 * There is no fuzzy or ellipsis matching here.
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

export interface PdfTextItemLike {
  str: string
  transform: number[]
  width: number
  height: number
}

export interface ReviewLocatorUnitLike {
  unit_id: string
  source_start: number
  source_end: number
  kind?: string
  parent_id?: string
  text?: string
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

const PDF_TEXT_ITEM_GEOMETRY_TOLERANCE = 0.5

function samePdfTextItemGeometry(left: PdfTextItemLike, right: PdfTextItemLike): boolean {
  if (left.transform.length < 6 || right.transform.length < 6) return false
  const leftGeometry = [...left.transform.slice(0, 6), left.width, left.height]
  const rightGeometry = [...right.transform.slice(0, 6), right.width, right.height]
  return leftGeometry.every((value, index) => Number.isFinite(value)
    && Number.isFinite(rightGeometry[index])
    && Math.abs(value - rightGeometry[index]) <= PDF_TEXT_ITEM_GEOMETRY_TOLERANCE)
}

/** Drop only overlaid copies of the same PDF text item; callers scope this per page. */
export function deduplicatePdfTextItems(items: ReadonlyArray<PdfTextItemLike>): PdfTextItemLike[] {
  const unique: PdfTextItemLike[] = []
  const candidatesByText = new Map<string, PdfTextItemLike[]>()
  for (const item of items) {
    const normalizedText = normalizeReviewText(item.str)
    const candidates = candidatesByText.get(normalizedText) || []
    if (candidates.some((candidate) => samePdfTextItemGeometry(candidate, item))) continue
    unique.push(item)
    candidates.push(item)
    candidatesByText.set(normalizedText, candidates)
  }
  return unique
}

/** Return every exact match so the locator can constrain repeated text. */
function findReviewQuoteMatches(renderedText: string, quote: string): ReviewQuoteMatch[] {
  const haystack = normalizeReviewText(renderedText)
  const needle = normalizeReviewText(quote)
  if (!needle) return []

  const matches: ReviewQuoteMatch[] = []
  let start = haystack.indexOf(needle)
  while (start >= 0) {
    matches.push({ start, end: start + needle.length })
    // Advancing by one also makes the helper correct for overlapping exact
    // matches. Callers must reject ambiguity rather than guessing.
    start = haystack.indexOf(needle, start + 1)
  }
  return matches
}

/** Return a location only when the exact match is unambiguous. */
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

function sourceUnitsContainingRange(locator: ReviewLocatorLike, sourceStart: number, sourceEnd: number): ReviewLocatorUnitLike[] {
  return (locator.units || [])
    .filter((unit) => {
      return Number.isFinite(unit.source_start)
        && Number.isFinite(unit.source_end)
        && unit.source_start <= sourceStart
        && unit.source_end >= sourceEnd
    })
    .sort((left, right) => {
      const leftSize = left.source_end - left.source_start
      const rightSize = right.source_end - right.source_start
      return leftSize - rightSize || left.source_start - right.source_start || left.unit_id.localeCompare(right.unit_id)
    })
}

function uniqueMatches(matches: ReviewQuoteMatch[]): ReviewQuoteMatch[] {
  const seen = new Set<string>()
  return matches.filter((match) => {
    const key = `${match.start}:${match.end}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function sourceUnitQuoteOffsets(unit: ReviewLocatorUnitLike, sourceStart: number, sourceEnd: number, quote: string): ReviewQuoteMatch | null {
  if (!unit.text) return null
  const unitRunes = Array.from(unit.text)
  const localStart = sourceStart - unit.source_start
  const localEnd = sourceEnd - unit.source_start
  if (!Number.isInteger(localStart) || !Number.isInteger(localEnd) || localStart < 0 || localEnd <= localStart || localEnd > unitRunes.length) return null
  const sourceQuote = unitRunes.slice(localStart, localEnd).join('')
  if (normalizeReviewText(sourceQuote) !== normalizeReviewText(quote)) return null
  const normalizedStart = normalizeReviewText(unitRunes.slice(0, localStart).join('')).length
  const normalizedEnd = normalizeReviewText(unitRunes.slice(0, localEnd).join('')).length
  if (normalizedEnd <= normalizedStart) return null
  return { start: normalizedStart, end: normalizedEnd }
}

function matchIsOnPages(match: ReviewQuoteMatch, pages: number[], renderedPageRanges: RenderedPageRange[]): boolean {
  if (!pages.length || !renderedPageRanges.length) return true
  const selectedPages = new Set(pages)
  const matchPages = renderedPageRanges
    .filter((range) => match.start < range.renderedEnd && match.end > range.renderedStart)
    .map((range) => range.page)
  return matchPages.length > 0 && matchPages.every((page) => selectedPages.has(page))
}

function matchesOnPages(matches: ReviewQuoteMatch[], pages: number[], renderedPageRanges: RenderedPageRange[]): ReviewQuoteMatch[] {
  if (!pages.length || !renderedPageRanges.length) return matches
  return matches.filter((match) => matchIsOnPages(match, pages, renderedPageRanges))
}

function renderedUnitRangesForUnit(unit: ReviewLocatorUnitLike, renderedUnitRanges: RenderedUnitRange[]): RenderedUnitRange[] {
  return renderedUnitRanges.filter((range) => {
    return range.unitId === unit.unit_id
      && range.sourceStart <= unit.source_start
      && range.sourceEnd >= unit.source_end
      && range.renderedEnd > range.renderedStart
  })
}

function matchesThroughRenderedUnit(
  unit: ReviewLocatorUnitLike,
  sourceStart: number,
  sourceEnd: number,
  quote: string,
  renderedText: string,
  exactMatches: ReviewQuoteMatch[],
  renderedUnitRanges: RenderedUnitRange[],
  pages: number[],
  renderedPageRanges: RenderedPageRange[],
): ReviewQuoteMatch[] {
  const sourceOffsets = sourceUnitQuoteOffsets(unit, sourceStart, sourceEnd, quote)
  const explicitRanges = renderedUnitRangesForUnit(unit, renderedUnitRanges)
  const normalizedRenderedText = normalizeReviewText(renderedText)
  const normalizedQuote = normalizeReviewText(quote)

  if (sourceOffsets && explicitRanges.length) {
    const explicitMatches = explicitRanges
      .map((range) => ({ start: range.renderedStart + sourceOffsets.start, end: range.renderedStart + sourceOffsets.end }))
      .filter((match, index) => {
        const range = explicitRanges[index]
        return match.start >= range.renderedStart
          && match.end <= range.renderedEnd
          && match.end <= normalizedRenderedText.length
          && normalizedRenderedText.slice(match.start, match.end) === normalizedQuote
          && matchIsOnPages(match, pages, renderedPageRanges)
      })
    if (explicitMatches.length) return uniqueMatches(explicitMatches)
  }

  if (sourceOffsets && unit.text) {
    const unitMatches = matchesOnPages(findReviewQuoteMatches(renderedText, unit.text), pages, renderedPageRanges)
    const mappedMatches = unitMatches
      .map((unitMatch) => ({ start: unitMatch.start + sourceOffsets.start, end: unitMatch.start + sourceOffsets.end }))
      .filter((match, index) => {
        const unitMatch = unitMatches[index]
        return match.start >= unitMatch.start
          && match.end <= unitMatch.end
          && match.end <= normalizedRenderedText.length
          && normalizedRenderedText.slice(match.start, match.end) === normalizedQuote
          && exactMatches.some((exactMatch) => exactMatch.start === match.start && exactMatch.end === match.end)
      })
    if (mappedMatches.length) return uniqueMatches(mappedMatches)
  }

  if (explicitRanges.length) {
    return uniqueMatches(exactMatches.filter((match) => {
      return explicitRanges.some((range) => {
        return match.start < range.renderedEnd
          && match.end > range.renderedStart
          && matchIsOnPages(match, pages, renderedPageRanges)
      })
    }))
  }

  return []
}

function unitsBySourceSize(units: ReviewLocatorUnitLike[]): ReviewLocatorUnitLike[][] {
  const groups: ReviewLocatorUnitLike[][] = []
  for (const unit of units) {
    const size = unit.source_end - unit.source_start
    const group = groups.find((candidate) => candidate[0] && candidate[0].source_end - candidate[0].source_start === size)
    if (group) group.push(unit)
    else groups.push([unit])
  }
  return groups
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
 * authoritative. Source unit text can map a source-relative quote offset to
 * the rendered document; optional rendered unit offsets take precedence.
 * If that mapping is unavailable, a page- or document-unique exact match is
 * accepted. Repeated matches remain explicitly ambiguous. When an older
 * server has no locator endpoint, the result remains unsupported; a text-only
 * match is never promoted to evidence.
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

  const containingUnits = sourceUnitsContainingRange(locator, resolvedSourceStart, resolvedSourceEnd)
  for (const sourceSizeGroup of unitsBySourceSize(containingUnits)) {
    const mappedMatches = uniqueMatches(sourceSizeGroup.flatMap((unit) => matchesThroughRenderedUnit(
      unit,
      resolvedSourceStart,
      resolvedSourceEnd,
      quote,
      renderedText,
      exactMatches,
      renderedUnitRanges,
      pages,
      renderedPageRanges,
    )))
    if (mappedMatches.length) {
      return finishResolution(mappedMatches, unitIds, pages, 'located', 'source-unit-mapped')
    }
  }

  const pageConstrained = pages.length > 0 && renderedPageRanges.length > 0
  const candidateMatches = pageConstrained
    ? matchesOnPages(exactMatches, pages, renderedPageRanges)
    : exactMatches
  if (candidateMatches.length || pageConstrained) {
    return finishResolution(candidateMatches, unitIds, pages, 'located', pageConstrained ? 'page-filtered' : 'document-unique')
  }
  return finishResolution(exactMatches, unitIds, pages, 'located', 'document-unique')
}

export type ReviewCollectionState = 'not_loaded' | 'empty' | 'populated'

/** Distinguish an omitted streaming field from a completed empty result. */
export function reviewCollectionState<T>(items: T[] | undefined): ReviewCollectionState {
  if (!Array.isArray(items)) return 'not_loaded'
  return items.length ? 'populated' : 'empty'
}
