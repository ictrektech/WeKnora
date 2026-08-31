<template>
  <section class="document-viewer">
    <header class="document-viewer__toolbar">
      <div class="document-viewer__file"><t-icon name="file-paste" /> <span>{{ fileName }}</span></div>
      <div class="document-viewer__controls">
        <button type="button" :aria-label="t('contractReview.previousPage')" @click="goToPage(currentPage - 1)"><t-icon name="chevron-up" /></button>
        <span>{{ currentPage }} / {{ pageCount || 1 }}</span>
        <button type="button" :aria-label="t('contractReview.nextPage')" @click="goToPage(currentPage + 1)"><t-icon name="chevron-down" /></button>
        <i />
        <button type="button" :aria-label="t('contractReview.zoomOut')" @click="setZoom(zoom - 0.1)"><t-icon name="zoom-out" /></button>
        <span>{{ Math.round(zoom * 100) }}%</span>
        <button type="button" :aria-label="t('contractReview.zoomIn')" @click="setZoom(zoom + 0.1)"><t-icon name="zoom-in" /></button>
      </div>
    </header>
    <div ref="scrollEl" class="document-viewer__scroll" @scroll="updateCurrentPage">
      <div v-if="loading" class="document-viewer__state"><t-loading size="small" /> {{ t('contractReview.loadingDocument') }}</div>
      <div v-else-if="error" class="document-viewer__state document-viewer__state--error">{{ error }}</div>
      <div v-show="!loading && fileType === '.pdf'" ref="pdfEl" class="document-viewer__pdf" :style="{ '--document-font-compensation': documentFontCompensation }" @click="onDocumentClick" />
      <div v-show="!loading && fileType === '.docx'" ref="docxEl" class="document-viewer__docx" :style="{ '--document-zoom': zoom, '--document-font-compensation': documentFontCompensation }" @click="onDocumentClick" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon as TIcon, Loading as TLoading } from 'tdesign-vue-next'
import { GlobalWorkerOptions, Util, getDocument, type PDFDocumentProxy } from 'pdfjs-dist'
import pdfWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'

import { getContractReviewDocument, type ReviewIssue } from '@/api/contract-review'
import { findReviewQuoteMatch, hasReviewQuoteMatch, normalizeReviewText } from './documentLinking'

GlobalWorkerOptions.workerSrc = `${pdfWorker}?pdfjs-worker=1`

const props = defineProps<{ reviewId: string; fileName: string; fileType: string; issues: ReviewIssue[]; selectedIssueId?: string }>()
const emit = defineEmits<{ markerClick: [issueId: string]; locateFailed: [] }>()
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

const normalize = normalizeReviewText

type NormalizedTextMap = {
  text: string
  offsets: Array<{ start: number; end: number }>
}

type PdfTextRange = {
  span: HTMLElement
  textNode: Text
  normalizedStart: number
  normalizedEnd: number
  offsets: Array<{ start: number; end: number }>
}

type PdfHighlightRect = { left: number; top: number; width: number; height: number }

function normalizedTextMap(value: string): NormalizedTextMap {
  let text = ''
  const offsets: Array<{ start: number; end: number }> = []
  for (let index = 0; index < value.length;) {
    const codePoint = value.codePointAt(index)
    if (codePoint === undefined) break
    const rawChar = String.fromCodePoint(codePoint)
    const normalizedChar = normalize(rawChar)
    if (normalizedChar) {
      text += normalizedChar
      for (let offset = 0; offset < normalizedChar.length; offset++) offsets.push({ start: index, end: index + rawChar.length })
    }
    index += rawChar.length
  }
  return { text, offsets }
}

function appFontScale() {
  if (typeof window === 'undefined') return 1
  const value = Number.parseFloat(window.getComputedStyle(document.documentElement).zoom)
  return Number.isFinite(value) && value > 0 ? value : 1
}

function updateDocumentFontCompensation() {
  documentFontCompensation.value = Number((1 / appFontScale()).toFixed(4))
}

async function load() {
  const generation = ++loadGeneration
  const previousPdf = pdf
  pdf = null
  pdfRenderGeneration++
  if (previousPdf) void previousPdf.destroy().catch(() => {})
  loading.value = true; error.value = ''
  pageCount.value = 0; currentPage.value = 1
  try {
    const data = await getContractReviewDocument(props.reviewId)
    if (generation !== loadGeneration) return
    if (props.fileType === '.pdf') await loadPdf(data, generation)
    else await loadDocx(data, generation)
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
  // The document container counteracts the application's CSS zoom, so the
  // PDF should be rasterized only for the physical display density. Including
  // the app font scale here would render an oversized bitmap and then make the
  // browser downsample it through the inverse document zoom, which softens text.
  const outputScale = Math.max(1, window.devicePixelRatio || 1)
  for (let pageNumber = 1; pageNumber <= documentProxy.numPages; pageNumber++) {
    if (!isCurrentRender()) return
    const page = await documentProxy.getPage(pageNumber)
    if (!isCurrentRender()) return
    const viewport = page.getViewport({ scale: zoom.value * 1.25 })
    const pageEl = document.createElement('section'); pageEl.className = 'pdf-page'; pageEl.dataset.page = String(pageNumber)
    pageEl.style.width = `${viewport.width}px`; pageEl.style.height = `${viewport.height}px`
    const canvas = document.createElement('canvas')
    canvas.width = Math.floor(viewport.width * outputScale)
    canvas.height = Math.floor(viewport.height * outputScale)
    canvas.style.width = `${viewport.width}px`
    canvas.style.height = `${viewport.height}px`
    const textLayer = document.createElement('div'); textLayer.className = 'pdf-text-layer'
    const context = canvas.getContext('2d'); if (!context) continue
    const highlightLayer = document.createElement('div'); highlightLayer.className = 'pdf-highlight-layer'
    pageEl.append(canvas, highlightLayer, textLayer); pdfEl.value.append(pageEl)
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
      const tx = Util.transform(viewport.transform, raw.transform)
      const span = document.createElement('span'); span.textContent = raw.str; span.dataset.text = raw.str
      const fontHeight = Math.hypot(tx[2], tx[3])
      span.style.left = `${tx[4]}px`; span.style.top = `${tx[5] - fontHeight}px`; span.style.fontSize = `${fontHeight}px`
      textLayer.append(span)
    }
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
}

function clearMarks() {
  docxEl.value?.querySelectorAll<HTMLElement>('.review-text-mark').forEach((el) => {
    const mark = el as HTMLElement
    mark.classList.remove('review-text-mark', 'review-text-mark--selected', 'review-text-mark--high', 'review-text-mark--medium', 'review-text-mark--low')
    mark.removeAttribute('data-issue-id')
  })
  pdfEl.value?.querySelectorAll<HTMLElement>('.pdf-review-highlight-group').forEach((group) => group.remove())
  pdfEl.value?.querySelectorAll<HTMLElement>('.pdf-text-layer span[data-issue-id]').forEach((span) => span.removeAttribute('data-issue-id'))
}

function applyIssueMarks() {
  clearMarks()
  for (const issue of props.issues || []) markIssue(issue)
  selectMark(props.selectedIssueId)
}

function markIssue(issue: ReviewIssue): HTMLElement | null {
  const needle = normalize(issue.original_quote)
  if (!needle) return null
  if (props.fileType === '.pdf' && pdfEl.value) {
    const existingGroup = pdfEl.value.querySelector<HTMLElement>(`.pdf-review-highlight-group[data-issue-id="${CSS.escape(issue.id)}"]`)
    if (existingGroup) return existingGroup
    for (const page of Array.from(pdfEl.value.querySelectorAll<HTMLElement>('.pdf-page'))) {
      const spans = Array.from(page.querySelectorAll<HTMLElement>('.pdf-text-layer span'))
      const ranges: PdfTextRange[] = []
      let joined = ''
      for (const span of spans) {
        const textNode = span.firstChild
        if (!(textNode instanceof Text)) continue
        const mapped = normalizedTextMap(span.dataset.text || '')
        if (!mapped.text) continue
        const normalizedStart = joined.length
        joined += mapped.text
        ranges.push({ span, textNode, normalizedStart, normalizedEnd: joined.length, offsets: mapped.offsets })
      }
      const match = findReviewQuoteMatch(joined, needle)
      if (!match) continue
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
        range.span.dataset.issueId = issue.id
      }

      if (!rectangles.length) continue
      const left = Math.min(...rectangles.map((rect) => rect.left))
      const top = Math.min(...rectangles.map((rect) => rect.top))
      const right = Math.max(...rectangles.map((rect) => rect.left + rect.width))
      const bottom = Math.max(...rectangles.map((rect) => rect.top + rect.height))
      const group = document.createElement('span')
      group.className = `pdf-review-highlight-group review-text-mark review-text-mark--${issue.risk_level}`
      group.dataset.issueId = issue.id
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
      return group
    }
  }
  if (props.fileType === '.docx' && docxEl.value) {
    const candidates = Array.from(docxEl.value.querySelectorAll<HTMLElement>('p, li, td'))
    const target = candidates.find((node) => hasReviewQuoteMatch(node.textContent || '', issue.original_quote))
    if (target) { target.classList.add('review-text-mark', `review-text-mark--${issue.risk_level}`); target.dataset.issueId = issue.id; return target }
  }
  return null
}

function selectMark(issueId?: string) {
  pdfEl.value?.querySelectorAll('.review-text-mark--selected').forEach((el) => el.classList.remove('review-text-mark--selected'))
  docxEl.value?.querySelectorAll('.review-text-mark--selected').forEach((el) => el.classList.remove('review-text-mark--selected'))
  if (!issueId) return
  const selector = props.fileType === '.pdf'
    ? `.pdf-review-highlight-group[data-issue-id="${CSS.escape(issueId)}"]`
    : `[data-issue-id="${CSS.escape(issueId)}"]`
  const target = (props.fileType === '.pdf' ? pdfEl.value : docxEl.value)?.querySelector<HTMLElement>(selector)
  target?.classList.add('review-text-mark--selected')
}

function locateIssue(issue: ReviewIssue) {
  const target = markIssue(issue)
  if (!target) { emit('locateFailed'); return false }
  selectMark(issue.id); target.scrollIntoView({ behavior: 'smooth', block: 'center' }); return true
}

function onDocumentClick(event: MouseEvent) {
  const target = (event.target as HTMLElement).closest<HTMLElement>('[data-issue-id]')
  if (target?.dataset.issueId) emit('markerClick', target.dataset.issueId)
}

function setZoom(value: number) {
  zoom.value = Math.min(2, Math.max(0.6, Number(value.toFixed(1))))
  if (props.fileType === '.pdf') void renderPdf().then(applyIssueMarks)
}

function goToPage(page: number) {
  const next = Math.min(pageCount.value || 1, Math.max(1, page)); currentPage.value = next
  const root = props.fileType === '.pdf' ? pdfEl.value : docxEl.value
  const pages = root?.querySelectorAll<HTMLElement>(props.fileType === '.pdf' ? '.pdf-page' : 'section')
  pages?.[next - 1]?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function updateCurrentPage() {
  const root = props.fileType === '.pdf' ? pdfEl.value : docxEl.value
  const pages = Array.from(root?.querySelectorAll<HTMLElement>(props.fileType === '.pdf' ? '.pdf-page' : 'section') || [])
  const top = scrollEl.value?.getBoundingClientRect().top || 0
  let best = 0, distance = Infinity
  pages.forEach((page, index) => { const next = Math.abs(page.getBoundingClientRect().top - top - 52); if (next < distance) { best = index; distance = next } })
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
watch(() => props.reviewId, load)
onMounted(() => { observeAppFontScale(); void load() })
onBeforeUnmount(() => {
  loadGeneration++
  pdfRenderGeneration++
  fontScaleObserver?.disconnect()
  const documentProxy = pdf
  pdf = null
  if (documentProxy) void documentProxy.destroy().catch(() => {})
})
defineExpose({ locateIssue, goToPage, setZoom })
</script>

<style scoped lang="less">
.document-viewer { height:100%; min-width:0; display:flex; flex-direction:column; color:var(--legal-text-primary); background:var(--legal-bg-hover); }
.document-viewer__toolbar { height:52px; padding:0 18px; display:flex; align-items:center; justify-content:space-between; background:var(--legal-bg-surface); border-bottom:1px solid var(--legal-border); }
.document-viewer__file { min-width:0; display:flex; align-items:center; gap:8px; font-size:13px; font-weight:600; span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; } }
.document-viewer__controls { display:flex; align-items:center; gap:7px; color:var(--legal-text-secondary); font-size:12px; button { width:28px; height:28px; border:0; border-radius:5px; color:inherit; background:transparent; cursor:pointer; &:hover{background:var(--legal-bg-hover);color:var(--legal-brand);} &:focus-visible{outline:2px solid var(--legal-ai);outline-offset:1px;} } i { width:1px;height:18px;background:var(--legal-border); margin:0 4px; } }
.document-viewer__scroll { min-height:0; flex:1; overflow:auto; padding:28px; }
.document-viewer__state { height:100%; display:flex; align-items:center; justify-content:center; gap:10px; color:var(--legal-text-secondary); &--error{color:var(--legal-risk);} }
.document-viewer__pdf { min-width:max-content; display:flex; flex-direction:column; align-items:center; gap:22px; zoom:var(--document-font-compensation, 1); }
:deep(.pdf-page) { position:relative; flex:none; background:var(--legal-bg-paper); box-shadow:0 3px 14px rgba(31,31,31,.1); canvas{position:absolute;inset:0;} }
:deep(.pdf-highlight-layer) { position:absolute; inset:0; z-index:1; pointer-events:none; }
:deep(.pdf-text-layer) { position:absolute;inset:0;z-index:2;overflow:hidden;line-height:1; span{position:absolute;white-space:pre;transform-origin:0 0;color:transparent;cursor:text;} ::selection{background:rgba(115,115,115,.22);} }
.document-viewer__docx { width:max-content; min-width:100%; zoom:var(--document-font-compensation, 1); transform-origin:top center; :deep(.docx-wrapper){padding:0;background:transparent;} :deep(section){margin:0 auto 22px!important; transform:scale(var(--document-zoom)); transform-origin:top center; margin-bottom:calc((var(--document-zoom) - 1) * 1120px + 22px)!important;} }
:deep(.review-text-mark) { background:rgba(115,115,115,.18)!important; box-shadow:inset 3px 0 var(--legal-ai); cursor:pointer!important; }
:deep(.review-text-mark--medium) { background:rgba(169,121,61,.18)!important; box-shadow:inset 3px 0 var(--legal-warning); }
:deep(.review-text-mark--high) { background:rgba(166,83,77,.18)!important; box-shadow:inset 3px 0 var(--legal-risk); }
:deep(.review-text-mark--selected) { outline:2px solid var(--legal-brand); outline-offset:1px; }
:deep(.review-text-mark--high.review-text-mark--selected) { outline-color:var(--legal-risk); }
:deep(.pdf-review-highlight-group) { position:absolute; display:block; background:transparent!important; box-shadow:none!important; cursor:pointer!important; }
:deep(.pdf-review-highlight-segment) { position:absolute; display:block; border-radius:2px; background:rgba(115,115,115,.18); }
:deep(.pdf-review-highlight-segment--medium) { background:rgba(169,121,61,.22); }
:deep(.pdf-review-highlight-segment--high) { background:rgba(166,83,77,.22); }
</style>
