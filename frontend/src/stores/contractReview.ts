import { defineStore } from 'pinia'
import { ref } from 'vue'

import { listModels, type ModelConfig } from '@/api/model'
import {
  bulkContractReviews, createContractReview, deleteContractReview, getContractReview, getContractReviewLocator, listContractReviewPlaybooks, listContractReviews,
  retryContractReview, startContractReview, streamContractReview, updateContractReview,
  uploadContractReviewDocument, type ContractReview, type ContractReviewBulkAction, type ContractReviewLocator, type LocatorLoadStatus, type ReviewPlaybook,
} from '@/api/contract-review'
import { contractReviewIsSettled, removeSuccessfulRows } from './legalState'

export interface ContractReviewLocatorState {
  status: LocatorLoadStatus
  locator: ContractReviewLocator | null
  error?: string
}

export const useContractReviewStore = defineStore('contractReview', () => {
  const tasks = ref<ContractReview[]>([])
  const current = ref<ContractReview | null>(null)
  const playbooks = ref<ReviewPlaybook[]>([])
  const models = ref<ModelConfig[]>([])
  const modelsLoading = ref(false)
  const loading = ref(false)
  const uploadProgress = ref(0)
  const locatorCache = ref<Record<string, ContractReviewLocator | null>>({})
  const locatorStates = ref<Record<string, ContractReviewLocatorState>>({})
  let streamController: AbortController | null = null
  let detailPollTimer: ReturnType<typeof setInterval> | null = null
  const locatorRequests = new Map<string, Promise<ContractReviewLocator | null>>()
  const locatorGenerations = new Map<string, number>()

  async function loadList(archived = false, silent = false) {
    if (!silent) loading.value = true
    try { tasks.value = (await listContractReviews(archived)).data || [] } finally { if (!silent) loading.value = false }
  }
  async function create() { const review = (await createContractReview()).data; current.value = review; return review }
  async function loadPlaybooks() { if (!playbooks.value.length) playbooks.value = (await listContractReviewPlaybooks()).data || [] }
  async function loadModels(force = false) {
    if (!force && models.value.length) return
    modelsLoading.value = true
    try { models.value = (await listModels('KnowledgeQA')) || [] } finally { modelsLoading.value = false }
  }
  async function load(id: string) {
    loading.value = true
    // Do not let a detail view render the previous contract while the new
    // route is loading. That can start a PDF render which becomes stale as
    // soon as the requested review arrives.
    if (current.value?.id !== id) {
      disconnect()
      current.value = null
    }
    try {
      current.value = (await getContractReview(id)).data
      if (current.value) seedLocatorFromReview(current.value)
      return current.value
    } finally { loading.value = false }
  }
  async function update(id: string, data: Parameters<typeof updateContractReview>[1]) { const review = (await updateContractReview(id, data)).data; if (current.value?.id === id) { current.value = review; seedLocatorFromReview(review) } return review }
  async function upload(id: string, file: File) { invalidateLocator(id); uploadProgress.value = 0; const review = (await uploadContractReviewDocument(id, file, (v) => uploadProgress.value = v)).data; current.value = review; seedLocatorFromReview(review); connect(id); void loadLocator(id); return review }
  async function start(id: string) { const review = (await startContractReview(id)).data; current.value = review; seedLocatorFromReview(review); connect(id); return review }
  async function retry(id: string) { invalidateLocator(id); const review = (await retryContractReview(id)).data; current.value = review; seedLocatorFromReview(review); connect(id); void loadLocator(id); return review }
  async function remove(id: string) { await deleteContractReview(id); tasks.value = tasks.value.filter((item) => item.id !== id); if (current.value?.id === id) current.value = null }
  async function bulk(action: ContractReviewBulkAction, ids: string[]) {
    const result = (await bulkContractReviews(action, ids)).data
    const succeeded = new Set(result.items.filter((item) => item.success).map((item) => item.id))
    tasks.value = removeSuccessfulRows(tasks.value, result)
    if (action === 'delete' && current.value && succeeded.has(current.value.id)) current.value = null
    return result
  }
  const isSettled = contractReviewIsSettled
  function stopDetailPolling() { if (detailPollTimer !== null) { clearInterval(detailPollTimer); detailPollTimer = null } }
  async function refreshCurrent(id: string) {
    try {
      const review = (await getContractReview(id)).data
      current.value = review
      seedLocatorFromReview(review)
      if (isSettled(review.status)) disconnect()
    } catch { /* SSE reconnect or a later poll will retry transient failures. */ }
  }
  function connect(id: string) {
    disconnect(); streamController = new AbortController()
    void streamContractReview(id, streamController.signal, (review) => { current.value = review; seedLocatorFromReview(review); if (isSettled(review.status)) disconnect() }).catch(() => undefined)
    // The SSE stream provides immediate clause updates. Polling the durable
    // snapshot covers proxies that buffer SSE and transient disconnects.
    void refreshCurrent(id)
    detailPollTimer = setInterval(() => { void refreshCurrent(id) }, 1500)
  }
  function disconnect() { streamController?.abort(); streamController = null; stopDetailPolling() }

  function setLocatorState(id: string, state: ContractReviewLocatorState) {
    locatorStates.value = { ...locatorStates.value, [id]: state }
  }

  function locatorFromPayload(payload: unknown): ContractReviewLocator | null {
    const response = payload as { data?: unknown } | null
    const raw = response && response.data !== undefined ? response.data : payload
    if (!raw || typeof raw !== 'object') return null
    const rawValue = raw as Record<string, unknown>
    const value = rawValue.locator && typeof rawValue.locator === 'object' ? rawValue.locator as Record<string, unknown> : rawValue
    let rawUnits: unknown = Array.isArray(value.units) ? value.units : value.source_units
    if (rawUnits === undefined) rawUnits = value.source_units_json
    if (typeof rawUnits === 'string') {
      try { rawUnits = JSON.parse(rawUnits) } catch { return null }
    }
    if (!Array.isArray(rawUnits)) return null
    const units = rawUnits
      .map((item) => {
        if (!item || typeof item !== 'object') return null
        const unit = item as Record<string, unknown>
        const sourceStart = Number(unit.source_start)
        const sourceEnd = Number(unit.source_end)
        const unitId = String(unit.unit_id ?? '')
        if (!unitId || !Number.isFinite(sourceStart) || !Number.isFinite(sourceEnd) || sourceEnd <= sourceStart) return null
        return {
          unit_id: unitId,
          kind: typeof unit.kind === 'string' ? unit.kind : undefined,
          parent_id: typeof unit.parent_id === 'string' ? unit.parent_id : undefined,
          page: Number.isFinite(Number(unit.page)) ? Number(unit.page) : undefined,
          source_start: sourceStart,
          source_end: sourceEnd,
          text: typeof unit.text === 'string' ? unit.text : typeof unit.source_text === 'string' ? unit.source_text : undefined,
          rendered_start: Number.isFinite(Number(unit.rendered_start)) ? Number(unit.rendered_start) : undefined,
          rendered_end: Number.isFinite(Number(unit.rendered_end)) ? Number(unit.rendered_end) : undefined,
        }
      })
      .filter((unit): unit is NonNullable<typeof unit> => unit !== null)
    if (!units.length) return null
    return {
      version: Number.isFinite(Number(value.version)) ? Number(value.version) : undefined,
      offset_unit: typeof value.offset_unit === 'string' ? value.offset_unit : undefined,
      source_hash: typeof value.source_hash === 'string' ? value.source_hash : undefined,
      source_length: Number.isFinite(Number(value.source_length)) ? Number(value.source_length) : undefined,
      review_id: typeof value.review_id === 'string' ? value.review_id : undefined,
      source_revision: typeof value.source_revision === 'string' ? value.source_revision : typeof value.source_hash === 'string' ? value.source_hash : undefined,
      source_text_hash: typeof value.source_text_hash === 'string' ? value.source_text_hash : typeof value.source_hash === 'string' ? value.source_hash : undefined,
      text: typeof value.text === 'string' ? value.text : undefined,
      source_text: typeof value.source_text === 'string' ? value.source_text : undefined,
      units,
    }
  }

  function seedLocatorFromReview(review: ContractReview) {
    if (!review.locator) return false
    const locator = locatorFromPayload(review.locator)
    if (!locator) return false
    locatorCache.value = { ...locatorCache.value, [review.id]: locator }
    setLocatorState(review.id, { status: 'ready', locator })
    return true
  }

  function isUnsupportedLocatorError(error: unknown) {
    const value = error as { status?: number; details?: unknown; error?: { details?: unknown } } | null
    if ([404, 405, 501].includes(Number(value?.status))) return true
    const details = value?.details ?? value?.error?.details
    return typeof details === 'string' && ['LOCATOR_UNSUPPORTED', 'NOT_FOUND', 'METHOD_NOT_ALLOWED'].includes(details)
  }

  async function loadLocator(id: string, force = false): Promise<ContractReviewLocator | null> {
    const previousState = locatorStates.value[id]
    if (!force && previousState && ['ready', 'unsupported'].includes(previousState.status)) return locatorCache.value[id] || null
    const existing = locatorRequests.get(id)
    if (existing) return existing

    const generation = locatorGenerations.get(id) || 0
    setLocatorState(id, { status: 'loading', locator: locatorCache.value[id] || null })
    // Initialize before the async closure so its finally block can safely
    // compare identity without triggering a temporal-dead-zone/type-check
    // error. The promise is replaced synchronously before any awaited work
    // can complete.
    let request: Promise<ContractReviewLocator | null> = Promise.resolve(null)
    request = (async () => {
      try {
        const payload = await getContractReviewLocator(id)
        const locator = locatorFromPayload(payload)
        if (!locator) {
          if (locatorGenerations.get(id) === generation) {
            locatorCache.value = { ...locatorCache.value, [id]: null }
            setLocatorState(id, { status: 'unsupported', locator: null })
          }
          return null
        }
        if (locatorGenerations.get(id) === generation) {
          locatorCache.value = { ...locatorCache.value, [id]: locator }
          setLocatorState(id, { status: 'ready', locator })
        }
        return locator
      } catch (error) {
        if (locatorGenerations.get(id) === generation) {
          if (isUnsupportedLocatorError(error)) {
            locatorCache.value = { ...locatorCache.value, [id]: null }
            setLocatorState(id, { status: 'unsupported', locator: null })
          } else {
            setLocatorState(id, { status: 'error', locator: locatorCache.value[id] || null, error: (error as { message?: string })?.message })
          }
        }
        return null
      } finally {
        if (locatorRequests.get(id) === request) locatorRequests.delete(id)
      }
    })()
    locatorRequests.set(id, request)
    return request
  }

  function invalidateLocator(id: string) {
    locatorGenerations.set(id, (locatorGenerations.get(id) || 0) + 1)
    locatorRequests.delete(id)
    locatorCache.value = { ...locatorCache.value }
    delete locatorCache.value[id]
    delete locatorStates.value[id]
    locatorCache.value = { ...locatorCache.value }
    locatorStates.value = { ...locatorStates.value }
  }

  return {
    tasks, current, playbooks, models, modelsLoading, loading, uploadProgress,
    locatorCache, locatorStates,
    loadList, loadPlaybooks, loadModels, create, load, update, upload, start, retry, remove, bulk, connect, disconnect,
    loadLocator, invalidateLocator,
  }
})
