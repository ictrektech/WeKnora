<template>
  <section class="document-viewer" :aria-busy="loading">
    <header class="document-viewer__toolbar">
      <div class="document-viewer__file"><t-icon name="file-paste" aria-hidden="true" /> <span>{{ fileName }}</span></div>
      <div class="document-viewer__controls">
        <button type="button" :disabled="loading || !!error || !pageCount" :aria-label="t('contractReview.previousPage')" @click="goToPage(currentPage - 1)"><t-icon name="chevron-up" aria-hidden="true" /></button>
        <span aria-live="polite">{{ currentPage }} / {{ pageCount || 1 }}</span>
        <button type="button" :disabled="loading || !!error || !pageCount" :aria-label="t('contractReview.nextPage')" @click="goToPage(currentPage + 1)"><t-icon name="chevron-down" aria-hidden="true" /></button>
        <i aria-hidden="true" />
        <button type="button" :disabled="loading || !!error" :aria-label="t('contractReview.zoomOut')" @click="setZoom(zoom - 0.1)"><t-icon name="zoom-out" aria-hidden="true" /></button>
        <span>{{ Math.round(zoom * 100) }}%</span>
        <button type="button" :disabled="loading || !!error" :aria-label="t('contractReview.zoomIn')" @click="setZoom(zoom + 0.1)"><t-icon name="zoom-in" aria-hidden="true" /></button>
      </div>
    </header>
    <div ref="scrollEl" class="document-viewer__scroll" :aria-label="t('contractReview.documentContent')" tabindex="0" @scroll="updateCurrentPage" @keydown="onDocumentKeydown">
      <div v-if="loading" class="document-viewer__state" aria-live="polite"><t-loading size="small" /> {{ t('contractReview.loadingDocument') }}</div>
      <div v-else-if="error" class="document-viewer__state document-viewer__state--error" role="alert">
        <p>{{ error }}</p>
        <button type="button" @click="load">{{ t('contractReview.retryDocument') }}</button>
      </div>
      <template v-if="!loading && !error">
        <div v-if="locatorStatus === 'loading'" class="document-viewer__locator-state" role="status">{{ t('contractReview.loadingEvidenceLocator') }}</div>
        <div v-else-if="locatorStatus === 'unsupported'" class="document-viewer__locator-state document-viewer__locator-state--warning" role="status">
          <span>{{ t('contractReview.evidenceLocatorUnsupported') }}</span>
          <button type="button" @click="emit('retryLocator')">{{ t('contractReview.retryEvidenceLocator') }}</button>
        </div>
        <div v-else-if="locatorStatus === 'error'" class="document-viewer__locator-state document-viewer__locator-state--error" role="alert">
          <span>{{ t('contractReview.evidenceLocatorFailed') }}</span>
          <button type="button" @click="emit('retryLocator')">{{ t('contractReview.retryEvidenceLocator') }}</button>
        </div>
        <div v-else-if="fileType !== '.pdf' && fileType !== '.docx'" class="document-viewer__state document-viewer__state--error" role="alert">
          <p>{{ t('contractReview.unsupportedDocumentType') }}</p>
          <button type="button" @click="load">{{ t('contractReview.retryDocument') }}</button>
        </div>
      </template>
      <!-- Keep the target mounted while loading. loadPdf/loadDocx run before the
           loading state is cleared and need a stable DOM container. -->
      <div v-show="!loading && !error && fileType === '.pdf'" ref="pdfEl" class="document-viewer__pdf" :style="{ '--document-font-compensation': documentFontCompensation }" @click="onDocumentClick" />
      <div v-show="!loading && !error && fileType === '.docx'" ref="docxEl" class="document-viewer__docx" :style="{ '--document-zoom': zoom, '--document-font-compensation': documentFontCompensation }" @click="onDocumentClick" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon as TIcon, Loading as TLoading } from 'tdesign-vue-next'
import { GlobalWorkerOptions, Util, getDocument, type PDFDocumentProxy } from 'pdfjs-dist'
import pdfWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'

import { getContractReviewDocument, type ContractReviewLocator, type EvidenceStatus, type LocatorLoadStatus, type ReviewIssue } from '@/api/contract-review'
import {
  buildNormalizedTextMap,
  resolveReviewEvidence,
  type EvidenceResolution,
  type RenderedPageRange,
  type RenderedUnitRange,
} from './documentLinking'

GlobalWorkerOptions.workerSrc = `${pdfWorker}?pdfjs-worker=1`

const props = withDefaults(defineProps<{
  reviewId: string
  fileName: string
  fileType: string
  issues?: ReviewIssue[]
  selectedIssueId?: string
  sourceRevision?: string
  locator?: ContractReviewLocator | null
  locatorStatus?: LocatorLoadStatus
}>(), {
  issues: () => [],
  locator: null,
  locatorStatus: 'idle',
})
const emit = defineEmits<{
  markerClick: [issueId: string]
  evidenceStatus: [payload: { issueId: string; status: EvidenceStatus }]
  locateFailed: [status: EvidenceStatus]
  retryLocator: []
}>()
const { t } = useI18n()
const scrollEl = ref<HTMLElement | null>(null)
const pdfEl = ref<HTMLElement | null>(null)
const docxEl = ref<HTMLElement | null>(null)
const loading = ref(true)
const error = ref('')
const zoom = ref(1)
const pageCount = ref(0)
const currentPage = ref(1)
const documentFontCompensation = ref(1)
let pdf: PDFDocumentProxy | null = null
let fontScaleObserver: MutationObserver | null = null
let loadGeneration = 0
let pdfRenderGeneration = 0
let documentText = ''
let renderedPageRanges: RenderedPageRange[] = []

type PdfTextRange = {
  span: HTMLElement
  textNode: Text
  normalizedStart: number
  normalizedEnd: number
  offsets: Array<{ start: number; end: number }>
  page: HTMLElement
  pageNumber: number
}

type DomTextRange = {
  textNode: Text
  normalizedStart: number
  normalizedEnd: number
  offsets: Array<{ start: number; end: number }>
}

type PdfHighlightRect = { left: number; top: number; width: number; height: number }

let pdfTextRanges: PdfTextRange[] = []
let docxTextRanges: DomTextRange[] = []
const evidenceResolutions = new Map<string, EvidenceResolution>()

function appFontScale() {
  if (typeof window === 'undefined') return 1
  const value = Number.parseFloat(window.getComputedStyle(document.documentElement).zoom)
  return Number.isFinite(value) && value > 0 ? value : 1
}

function updateDocumentFontCompensation() {
  documentFontCompensation.value = Number((1 / appFontScale()).toFixed(4))
}

function issueEvidence(issue: ReviewIssue) {
  if (Array.isArray(issue.evidence)) return issue.evidence[0]
  return issue.evidence
}

function issueSourceStart(issue: ReviewIssue) {
  if (Number.isInteger(issue.source_start) && Number.isInteger(issue.source_end) && issue.source_start >= 0 && issue.source_end > issue.source_start) return issue.source_start
  return issueEvidence(issue)?.source_start
}

function issueSourceEnd(issue: ReviewIssue) {
  if (Number.isInteger(issue.source_start) && Number.isInteger(issue.source_end) && issue.source_start >= 0 && issue.source_end > issue.source_start) return issue.source_end
  return issueEvidence(issue)?.source_end
}

function issueSourceRevision(issue: ReviewIssue) {
  return issue.source_revision ?? issueEvidence(issue)?.source_revision ?? props.sourceRevision
}

function renderedUnitRanges(): RenderedUnitRange[] {
  return (props.locator?.units || [])
    .filter((unit) => Number.isFinite(unit.rendered_start) && Number.isFinite(unit.rendered_end) && (unit.rendered_end as number) > (unit.rendered_start as number))
    .map((unit) => ({
      unitId: unit.unit_id,
      sourceStart: unit.source_start,
      sourceEnd: unit.source_end,
      renderedStart: unit.rendered_start as number,
      renderedEnd: unit.rendered_end as number,
      page: unit.page,
    }))
}

function resolutionFor(issue: ReviewIssue): EvidenceResolution {
  if (error.value) {
    return { status: 'error', matches: [], unitIds: [], reason: 'document-error' }
  }
  if (loading.value || !documentText) {
    return { status: 'pending', matches: [], unitIds: [], reason: 'document-loading' }
  }
  return resolveReviewEvidence({
    renderedText: documentText,
    quote: issue.original_quote,
    sourceStart: issueSourceStart(issue),
    sourceEnd: issueSourceEnd(issue),
    expectedRevision: props.sourceRevision,
    evidenceRevision: issueSourceRevision(issue),
    locator: props.locator,
    locatorStatus: props.locatorStatus,
    renderedUnitRanges: renderedUnitRanges(),
    renderedPageRanges,
  })
}

function evidenceAriaLabel(issue: ReviewIssue, status: EvidenceStatus) {
  return `${issue.title}: ${t(`contractReview.evidence.${status}`)}`
}

function setEvidenceStatus(issue: ReviewIssue, resolution: EvidenceResolution) {
  evidenceResolutions.set(issue.id, resolution)
  emit('evidenceStatus', {
    issueId: issue.id,
    status: resolution.status,
  })
}

async function load() {
  const generation = ++loadGeneration
  const previousPdf = pdf
  pdf = null
  pdfRenderGeneration++
  if (previousPdf) void previousPdf.destroy().catch(() => {})
  loading.value = true
  error.value = ''
  documentText = ''
  pdfTextRanges = []
  docxTextRanges = []
  renderedPageRanges = []
  pageCount.value = 0
  currentPage.value = 1
  evidenceResolutions.clear()
  try {
    const data = await getContractReviewDocument(props.reviewId)
    if (generation !== loadGeneration) return
    if (props.fileType === '.pdf') await loadPdf(data, generation)
    else if (props.fileType === '.docx') await loadDocx(data, generation)
    else throw new Error(t('contractReview.unsupportedDocumentType'))
  } catch (cause: any) {
    if (generation === loadGeneration) error.value = cause?.message || t('contractReview.documentLoadFailed')
  } finally {
    if (generation !== loadGeneration) return
    loading.value = false
    await nextTick()
    if (generation === loadGeneration) applyIssueMarks()
  }
}

async function loadPdf(data: ArrayBuffer, generation: number) {
  if (generation !== loadGeneration || !pdfEl.value) return
  const loadedPdf = await getDocument({ data: new Uint8Array(data.slice(0)) }).promise
  if (generation !== loadGeneration || !pdfEl.value) {
    await loadedPdf.destroy().catch(() => {})
    return
  }
  pdf = loadedPdf
  pageCount.value = loadedPdf.numPages
  await renderPdf(loadedPdf, generation)
}

async function renderPdf(documentProxy: PDFDocumentProxy | null = pdf, generation = loadGeneration) {
  if (!documentProxy || !pdfEl.value || generation !== loadGeneration || pdf !== documentProxy) return
  const renderGeneration = ++pdfRenderGeneration
  const isCurrentRender = () => renderGeneration === pdfRenderGeneration
    && generation === loadGeneration
    && pdf === documentProxy
    && pdfEl.value !== null
  pdfEl.value.innerHTML = ''
  pdfTextRanges = []
  documentText = ''
  renderedPageRanges = []
  const outputScale = Math.max(1, window.devicePixelRatio || 1)
  for (let pageNumber = 1; pageNumber <= documentProxy.numPages; pageNumber++) {
    if (!isCurrentRender()) return
    const page = await documentProxy.getPage(pageNumber)
    if (!isCurrentRender()) return
    const viewport = page.getViewport({ scale: zoom.value * 1.25 })
    const pageEl = document.createElement('section')
    pageEl.className = 'pdf-page'
    pageEl.dataset.page = String(pageNumber)
    pageEl.style.width = `${viewport.width}px`
    pageEl.style.height = `${viewport.height}px`
    const canvas = document.createElement('canvas')
    canvas.width = Math.floor(viewport.width * outputScale)
    canvas.height = Math.floor(viewport.height * outputScale)
    canvas.style.width = `${viewport.width}px`
    canvas.style.height = `${viewport.height}px`
    const textLayer = document.createElement('div')
    textLayer.className = 'pdf-text-layer'
    const context = canvas.getContext('2d')
    if (!context) continue
    const highlightLayer = document.createElement('div')
    highlightLayer.className = 'pdf-highlight-layer'
    pageEl.append(canvas, highlightLayer, textLayer)
    pdfEl.value.append(pageEl)
    await page.render({
      canvas,
      canvasContext: context,
      viewport,
      transform: outputScale === 1 ? undefined : [outputScale, 0, 0, outputScale, 0, 0],
    }).promise
    if (!isCurrentRender()) return
    const content = await page.getTextContent()
    if (!isCurrentRender()) return
    for (const raw of content.items as any[]) {
      if (!('str' in raw) || !raw.str) continue
      const mapped = buildNormalizedTextMap(raw.str)
      if (!mapped.text) continue
      const tx = Util.transform(viewport.transform, raw.transform)
      const span = document.createElement('span')
      span.textContent = raw.str
      span.dataset.text = raw.str
      const fontHeight = Math.hypot(tx[2], tx[3])
      span.style.left = `${tx[4]}px`
      span.style.top = `${tx[5] - fontHeight}px`
      span.style.fontSize = `${fontHeight}px`
      textLayer.append(span)
      const normalizedStart = documentText.length
      documentText += mapped.text
      pdfTextRanges.push({ span, textNode: span.firstChild as Text, normalizedStart, normalizedEnd: documentText.length, offsets: mapped.offsets, page: pageEl, pageNumber })
    }
    renderedPageRanges.push({ page: pageNumber, renderedStart: pdfTextRanges.find((range) => range.page === pageEl)?.normalizedStart ?? documentText.length, renderedEnd: documentText.length })
  }
}

async function loadDocx(data: ArrayBuffer, generation: number) {
  if (generation !== loadGeneration || !docxEl.value) return
  const { renderAsync } = await import('docx-preview')
  if (generation !== loadGeneration || !docxEl.value) return
  docxEl.value.innerHTML = ''
  await renderAsync(new Blob([data]), docxEl.value, undefined, { className: 'contract-docx', inWrapper: true, breakPages: true, ignoreLastRenderedPageBreak: true, useBase64URL: true })
  if (generation !== loadGeneration || !docxEl.value) return
  pageCount.value = Math.max(1, docxEl.value.querySelectorAll('section').length)
  buildDocxTextIndex()
}

function buildDocxTextIndex() {
  if (!docxEl.value) return
  documentText = ''
  docxTextRanges = []
  renderedPageRanges = []
  const pageStarts = new Map<number, number>()
  const pageEnds = new Map<number, number>()
  const sections = Array.from(docxEl.value.querySelectorAll<HTMLElement>('section'))
  const walker = document.createTreeWalker(docxEl.value, NodeFilter.SHOW_TEXT)
  let node = walker.nextNode()
  while (node) {
    const textNode = node as Text
    const mapped = buildNormalizedTextMap(textNode.data)
    if (mapped.text) {
      const normalizedStart = documentText.length
      documentText += mapped.text
      docxTextRanges.push({ textNode, normalizedStart, normalizedEnd: documentText.length, offsets: mapped.offsets })
      const section = textNode.parentElement?.closest('section')
      const page = section ? sections.indexOf(section) + 1 : 1
      if (!pageStarts.has(page)) pageStarts.set(page, normalizedStart)
      pageEnds.set(page, documentText.length)
    }
    node = walker.nextNode()
  }
  renderedPageRanges = Array.from(pageStarts.keys()).map((page) => ({ page, renderedStart: pageStarts.get(page) as number, renderedEnd: pageEnds.get(page) as number }))
}

function unwrapDocxMarks() {
  if (!docxEl.value) return
  for (const mark of Array.from(docxEl.value.querySelectorAll<HTMLElement>('[data-review-mark="true"]'))) {
    const parent = mark.parentNode
    if (!parent) continue
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
  }
  docxEl.value.querySelectorAll<HTMLElement>('[data-issue-id]').forEach((el) => {
    el.removeAttribute('data-issue-id')
    el.classList.remove('review-text-mark', 'review-text-mark--selected', 'review-text-mark--high', 'review-text-mark--medium', 'review-text-mark--low')
  })
}

function clearMarks() {
  unwrapDocxMarks()
  pdfEl.value?.querySelectorAll<HTMLElement>('.pdf-review-highlight-group').forEach((group) => group.remove())
  pdfEl.value?.querySelectorAll<HTMLElement>('.pdf-text-layer span[data-issue-id]').forEach((span) => span.removeAttribute('data-issue-id'))
}

function applyIssueMarks() {
  clearMarks()
  evidenceResolutions.clear()
  if (props.fileType === '.docx' && docxEl.value) buildDocxTextIndex()
  for (const issue of props.issues || []) markIssue(issue)
  selectMark(props.selectedIssueId)
}

function markPdfIssue(issue: ReviewIssue, match: { start: number; end: number }, status: EvidenceStatus): HTMLElement | null {
  if (!pdfEl.value) return null
  const groups: HTMLElement[] = []
  const escapedId = CSS.escape(issue.id)
  if (pdfEl.value.querySelector(`.pdf-review-highlight-group[data-issue-id="${escapedId}"]`)) {
    return pdfEl.value.querySelector<HTMLElement>(`.pdf-review-highlight-group[data-issue-id="${escapedId}"]`)
  }
  const label = evidenceAriaLabel(issue, status)
  for (const page of Array.from(pdfEl.value.querySelectorAll<HTMLElement>('.pdf-page'))) {
    const ranges = pdfTextRanges.filter((range) => range.page === page && match.start < range.normalizedEnd && match.end > range.normalizedStart)
    if (!ranges.length) continue
    const highlightLayer = page.querySelector<HTMLElement>('.pdf-highlight-layer')
    if (!highlightLayer) continue
    const pageRect = page.getBoundingClientRect()
    const pageScaleX = page.offsetWidth ? pageRect.width / page.offsetWidth : 1
    const pageScaleY = page.offsetHeight ? pageRect.height / page.offsetHeight : 1
    const rectangles: PdfHighlightRect[] = []
    for (const range of ranges) {
      const localStart = Math.max(match.start - range.normalizedStart, 0)
      const localEnd = Math.min(match.end - range.normalizedStart, range.normalizedEnd - range.normalizedStart)
      if (localStart >= localEnd) continue
      const rawStart = range.offsets[localStart]?.start
      const rawEnd = range.offsets[localEnd - 1]?.end
      if (rawStart === undefined || rawEnd === undefined || rawStart >= rawEnd) continue
      const textRange = document.createRange()
      textRange.setStart(range.textNode, rawStart)
      textRange.setEnd(range.textNode, rawEnd)
      for (const rect of Array.from(textRange.getClientRects())) {
        if (rect.width <= 0 || rect.height <= 0) continue
        rectangles.push({
          left: (rect.left - pageRect.left) / pageScaleX,
          top: (rect.top - pageRect.top) / pageScaleY,
          width: rect.width / pageScaleX,
          height: rect.height / pageScaleY,
        })
      }
    }
    if (!rectangles.length) continue
    const left = Math.min(...rectangles.map((rect) => rect.left))
    const top = Math.min(...rectangles.map((rect) => rect.top))
    const right = Math.max(...rectangles.map((rect) => rect.left + rect.width))
    const bottom = Math.max(...rectangles.map((rect) => rect.top + rect.height))
    const group = document.createElement('span')
    group.className = `pdf-review-highlight-group review-text-mark review-text-mark--${issue.risk_level}`
    group.dataset.issueId = issue.id
    group.dataset.reviewMark = 'true'
    group.setAttribute('role', 'button')
    group.setAttribute('tabindex', '0')
    group.setAttribute('aria-label', label)
    group.style.left = `${left}px`
    group.style.top = `${top}px`
    group.style.width = `${right - left}px`
    group.style.height = `${bottom - top}px`
    rectangles.forEach((rect) => {
      const segment = document.createElement('span')
      segment.className = `pdf-review-highlight-segment pdf-review-highlight-segment--${issue.risk_level}`
      segment.style.left = `${rect.left - left}px`
      segment.style.top = `${rect.top - top}px`
      segment.style.width = `${rect.width}px`
      segment.style.height = `${rect.height}px`
      group.append(segment)
    })
    highlightLayer.append(group)
    groups.push(group)
  }
  return groups[0] || null
}

function markDocxIssue(issue: ReviewIssue, match: { start: number; end: number }, status: EvidenceStatus): HTMLElement | null {
  const label = evidenceAriaLabel(issue, status)
  const marks: HTMLElement[] = []
  for (const range of docxTextRanges) {
    const localStart = Math.max(match.start - range.normalizedStart, 0)
    const localEnd = Math.min(match.end - range.normalizedStart, range.normalizedEnd - range.normalizedStart)
    if (localStart >= localEnd) continue
    const rawStart = range.offsets[localStart]?.start
    const rawEnd = range.offsets[localEnd - 1]?.end
    if (rawStart === undefined || rawEnd === undefined || rawStart >= rawEnd) continue
    const domRange = document.createRange()
    domRange.setStart(range.textNode, rawStart)
    domRange.setEnd(range.textNode, rawEnd)
    const mark = document.createElement('mark')
    mark.className = `review-text-mark review-text-mark--${issue.risk_level}`
    mark.dataset.issueId = issue.id
    mark.dataset.reviewMark = 'true'
    mark.setAttribute('role', 'button')
    mark.setAttribute('tabindex', '0')
    mark.setAttribute('aria-label', label)
    try {
      domRange.surroundContents(mark)
      marks.push(mark)
    } catch {
      // A browser can reject a range after a previous overlapping finding
      // changed the text tree. Ambiguous DOM structure is not a reason to
      // widen the highlight to the whole paragraph.
    }
  }
  return marks[0] || null
}

function markIssue(issue: ReviewIssue): HTMLElement | null {
  const resolution = resolutionFor(issue)
  setEvidenceStatus(issue, resolution)
  // Legacy text-only matches are intentionally never promoted to a visual
  // highlight. Only a current, validated locator may authorize marking text.
  if (resolution.status !== 'located' || resolution.matches.length !== 1) return null
  const match = resolution.matches[0]
  if (props.fileType === '.pdf') return markPdfIssue(issue, match, resolution.status)
  if (props.fileType === '.docx') return markDocxIssue(issue, match, resolution.status)
  return null
}

function selectMark(issueId?: string) {
  pdfEl.value?.querySelectorAll('.review-text-mark--selected').forEach((el) => el.classList.remove('review-text-mark--selected'))
  docxEl.value?.querySelectorAll('.review-text-mark--selected').forEach((el) => el.classList.remove('review-text-mark--selected'))
  if (!issueId) return
  const selector = `[data-issue-id="${CSS.escape(issueId)}"]`
  const roots = [pdfEl.value, docxEl.value].filter((root): root is HTMLElement => root !== null)
  roots.forEach((root) => root.querySelectorAll<HTMLElement>(selector).forEach((target) => target.classList.add('review-text-mark--selected')))
}

function locateIssue(issue: ReviewIssue) {
  const target = markIssue(issue)
  const resolution = evidenceResolutions.get(issue.id)
  if (resolution?.status === 'pending') return false
  if (!target) {
    emit('locateFailed', resolution?.status || 'not_found')
    return false
  }
  selectMark(issue.id)
  target.scrollIntoView({ behavior: 'smooth', block: 'center' })
  target.focus({ preventScroll: true })
  return true
}

function issueIdFromTarget(event: Event) {
  const target = event.target
  return target instanceof Element ? target.closest<HTMLElement>('[data-issue-id]')?.dataset.issueId : undefined
}

function onDocumentClick(event: MouseEvent) {
  const issueId = issueIdFromTarget(event)
  if (issueId) emit('markerClick', issueId)
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  const issueId = issueIdFromTarget(event)
  if (!issueId) return
  event.preventDefault()
  emit('markerClick', issueId)
}

function setZoom(value: number) {
  zoom.value = Math.min(2, Math.max(0.6, Number(value.toFixed(1))))
  if (props.fileType === '.pdf' && !error.value) void renderPdf().then(applyIssueMarks)
}

function goToPage(page: number) {
  const next = Math.min(pageCount.value || 1, Math.max(1, page))
  currentPage.value = next
  const root = props.fileType === '.pdf' ? pdfEl.value : docxEl.value
  const pages = root?.querySelectorAll<HTMLElement>(props.fileType === '.pdf' ? '.pdf-page' : 'section')
  pages?.[next - 1]?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function updateCurrentPage() {
  const root = props.fileType === '.pdf' ? pdfEl.value : docxEl.value
  const pages = Array.from(root?.querySelectorAll<HTMLElement>(props.fileType === '.pdf' ? '.pdf-page' : 'section') || [])
  if (!pages.length) return
  const top = scrollEl.value?.getBoundingClientRect().top || 0
  let best = 0
  let distance = Infinity
  pages.forEach((page, index) => {
    const next = Math.abs(page.getBoundingClientRect().top - top - 52)
    if (next < distance) { best = index; distance = next }
  })
  currentPage.value = best + 1
}

function observeAppFontScale() {
  if (typeof MutationObserver === 'undefined') return
  let previous = appFontScale()
  updateDocumentFontCompensation()
  fontScaleObserver = new MutationObserver(() => {
    const next = appFontScale()
    if (next === previous) return
    previous = next
    updateDocumentFontCompensation()
    if (pdf) void renderPdf().then(applyIssueMarks)
  })
  fontScaleObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['style'] })
}

watch(() => props.selectedIssueId, selectMark)
watch(() => props.issues, applyIssueMarks, { deep: true })
watch(() => [props.reviewId, props.fileName, props.fileType], load)
watch(() => [props.locator, props.locatorStatus, props.sourceRevision], applyIssueMarks, { deep: true })
onMounted(() => { observeAppFontScale(); void load() })
onBeforeUnmount(() => {
  loadGeneration++
  pdfRenderGeneration++
  fontScaleObserver?.disconnect()
  const documentProxy = pdf
  pdf = null
  if (documentProxy) void documentProxy.destroy().catch(() => {})
})
defineExpose({ locateIssue, goToPage, setZoom, load })
</script>

<style scoped lang="less">
.document-viewer { height:100%; min-width:0; display:flex; flex-direction:column; color:var(--legal-text-primary); background:var(--legal-bg-hover); }
.document-viewer__toolbar { height:52px; padding:0 18px; display:flex; align-items:center; justify-content:space-between; background:var(--legal-bg-surface); border-bottom:1px solid var(--legal-border); }
.document-viewer__file { min-width:0; display:flex; align-items:center; gap:8px; font-size:13px; font-weight:600; span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; } }
.document-viewer__controls { display:flex; align-items:center; gap:7px; color:var(--legal-text-secondary); font-size:12px; button { width:28px; height:28px; border:0; border-radius:5px; color:inherit; background:transparent; cursor:pointer; &:hover:not(:disabled){background:var(--legal-bg-hover);color:var(--legal-brand);} &:focus-visible{outline:2px solid var(--legal-ai);outline-offset:1px;} &:disabled{opacity:.45;cursor:not-allowed;} } i { width:1px;height:18px;background:var(--legal-border); margin:0 4px; } }
.document-viewer__scroll { min-height:0; flex:1; overflow:auto; padding:28px; &:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:-2px;} }
.document-viewer__state { min-height:100%; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:10px; color:var(--legal-text-secondary); &--error{color:var(--legal-risk);} p{margin:0;} button{padding:7px 12px;border:1px solid var(--legal-border);border-radius:4px;color:var(--legal-text-primary);background:var(--legal-bg-surface);cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:1px;}} }
.document-viewer__locator-state { position:sticky; top:0; z-index:3; display:flex; align-items:center; justify-content:center; gap:10px; margin:-18px auto 12px; padding:7px 10px; color:var(--legal-text-secondary); background:var(--legal-bg-surface); border:1px solid var(--legal-border); border-radius:4px; font-size:11px; button{padding:4px 8px;border:1px solid var(--legal-border);border-radius:4px;color:inherit;background:transparent;cursor:pointer;&:focus-visible{outline:2px solid var(--legal-focus-ring);outline-offset:1px;}} &--warning{color:var(--legal-warning-strong);border-color:var(--legal-warning);} &--error{color:var(--legal-risk-strong);border-color:var(--legal-risk);} }
.document-viewer__pdf { min-width:max-content; display:flex; flex-direction:column; align-items:center; gap:22px; zoom:var(--document-font-compensation, 1); }
:deep(.pdf-page) { position:relative; flex:none; background:var(--legal-bg-paper); box-shadow:0 3px 14px rgba(31,31,31,.1); canvas{position:absolute;inset:0;} }
:deep(.pdf-highlight-layer) {
  // Keep the interactive highlight groups above the transparent PDF text
  // layer. Otherwise a click on a visible highlight lands on the text span,
  // which has no data-issue-id and cannot select the matching review item.
  position:absolute;
  inset:0;
  z-index:3;
  pointer-events:none;
}
:deep(.pdf-text-layer) { position:absolute;inset:0;z-index:2;overflow:hidden;line-height:1; span{position:absolute;white-space:pre;transform-origin:0 0;color:transparent;cursor:text;} ::selection{background:rgba(115,115,115,.22);} }
.document-viewer__docx { width:max-content; min-width:100%; zoom:var(--document-font-compensation, 1); transform-origin:top center; :deep(.docx-wrapper){padding:0;background:transparent;} :deep(section){margin:0 auto 22px!important; transform:scale(var(--document-zoom)); transform-origin:top center; margin-bottom:calc((var(--document-zoom) - 1) * 1120px + 22px)!important;} }
:deep(.review-text-mark) { background:rgba(115,115,115,.18)!important; box-shadow:inset 3px 0 var(--legal-ai); cursor:pointer!important; }
:deep(mark.review-text-mark) { color:inherit; }
:deep(.review-text-mark--medium) { background:rgba(169,121,61,.18)!important; box-shadow:inset 3px 0 var(--legal-warning); }
:deep(.review-text-mark--high) { background:rgba(166,83,77,.18)!important; box-shadow:inset 3px 0 var(--legal-risk); }
:deep(.review-text-mark--selected) { outline:2px solid var(--legal-brand); outline-offset:1px; }
:deep(.review-text-mark--high.review-text-mark--selected) { outline-color:var(--legal-risk); }
:deep(.pdf-review-highlight-group) { position:absolute; display:block; background:transparent!important; box-shadow:none!important; cursor:pointer!important; pointer-events:auto; }
:deep(.pdf-review-highlight-segment) { position:absolute; display:block; border-radius:2px; background:rgba(115,115,115,.18); }
:deep(.pdf-review-highlight-segment--medium) { background:rgba(169,121,61,.22); }
:deep(.pdf-review-highlight-segment--high) { background:rgba(166,83,77,.22); }
</style>
