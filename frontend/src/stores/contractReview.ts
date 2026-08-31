import { defineStore } from 'pinia'
import { ref } from 'vue'

import {
  bulkContractReviews, createContractReview, deleteContractReview, getContractReview, listContractReviewPlaybooks, listContractReviews,
  retryContractReview, startContractReview, streamContractReview, updateContractReview,
  uploadContractReviewDocument, type ContractReview, type ContractReviewBulkAction, type ReviewPlaybook,
} from '@/api/contract-review'
import { contractReviewIsSettled, removeSuccessfulRows } from './legalState'

export const useContractReviewStore = defineStore('contractReview', () => {
  const tasks = ref<ContractReview[]>([])
  const current = ref<ContractReview | null>(null)
  const playbooks = ref<ReviewPlaybook[]>([])
  const loading = ref(false)
  const uploadProgress = ref(0)
  let streamController: AbortController | null = null
  let detailPollTimer: ReturnType<typeof setInterval> | null = null

  async function loadList(archived = false, silent = false) {
    if (!silent) loading.value = true
    try { tasks.value = (await listContractReviews(archived)).data || [] } finally { if (!silent) loading.value = false }
  }
  async function create() { const review = (await createContractReview()).data; current.value = review; return review }
  async function loadPlaybooks() { if (!playbooks.value.length) playbooks.value = (await listContractReviewPlaybooks()).data || [] }
  async function load(id: string) {
    loading.value = true
    // Do not let a detail view render the previous contract while the new
    // route is loading. That can start a PDF render which becomes stale as
    // soon as the requested review arrives.
    if (current.value?.id !== id) {
      disconnect()
      current.value = null
    }
    try { current.value = (await getContractReview(id)).data; return current.value } finally { loading.value = false }
  }
  async function update(id: string, data: Parameters<typeof updateContractReview>[1]) { const review = (await updateContractReview(id, data)).data; if (current.value?.id === id) current.value = review; return review }
  async function upload(id: string, file: File) { uploadProgress.value = 0; const review = (await uploadContractReviewDocument(id, file, (v) => uploadProgress.value = v)).data; current.value = review; connect(id); return review }
  async function start(id: string) { const review = (await startContractReview(id)).data; current.value = review; connect(id); return review }
  async function retry(id: string) { const review = (await retryContractReview(id)).data; current.value = review; connect(id); return review }
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
      if (isSettled(review.status)) disconnect()
    } catch { /* SSE reconnect or a later poll will retry transient failures. */ }
  }
  function connect(id: string) {
    disconnect(); streamController = new AbortController()
    void streamContractReview(id, streamController.signal, (review) => { current.value = review; if (isSettled(review.status)) disconnect() }).catch(() => undefined)
    // The SSE stream provides immediate clause updates. Polling the durable
    // snapshot covers proxies that buffer SSE and transient disconnects.
    void refreshCurrent(id)
    detailPollTimer = setInterval(() => { void refreshCurrent(id) }, 1500)
  }
  function disconnect() { streamController?.abort(); streamController = null; stopDetailPolling() }

  return { tasks, current, playbooks, loading, uploadProgress, loadList, loadPlaybooks, create, load, update, upload, start, retry, remove, bulk, connect, disconnect }
})
