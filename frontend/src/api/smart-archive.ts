import { fetchEventSource } from '@microsoft/fetch-event-source'
import { getApiBaseUrl } from '@/utils/api-base'
import { del, get, getDown, patch, post, postUpload } from '@/utils/request'

export const MANAGED_SMART_ARCHIVE_KB_MARKER = '[weknora-managed-smart-archive]'

export type ArchiveDocumentType = 'contract' | 'loan_agreement' | 'outbound_order' | 'return_order' | 'renewal' | 'payment' | 'delivery' | 'other'
export type ArchiveExtractionStatus = 'uploading' | 'parsing' | 'extracting' | 'linking' | 'needs_review' | 'completed' | 'failed' | 'canceled'
export type ArchiveMirrorStatus = 'not_started' | 'pending' | 'processing' | 'submitted' | 'failed'
export type ArchiveReminderStatus = 'draft' | 'active' | 'snoozed' | 'handled' | 'canceled'
export type ArchiveBulkAction = 'archive' | 'restore' | 'delete' | 'purge' | 'ignore'

export interface ArchiveEvidence { id: string; document_id: string; knowledge_id?: string; chunk_id?: string; field_name: string; value: string; confidence: number; quote: string; locator_kind: string; locator: Record<string, unknown>; source_start: number; source_end: number; is_manual: boolean }
export interface ArchiveCustomer { id: string; name: string; normalized: string; aliases: string[]; notes?: string }
export interface ArchiveDocument { id: string; import_batch_id?: string; title: string; file_name: string; file_type: string; file_size: number; document_type: ArchiveDocumentType; business_type: string; customer_id?: string; agreement_number: string; signed_at?: string; effective_at?: string; expires_at?: string; return_due_at?: string; returned_at?: string; renewed_at?: string; amount: number; currency: string; extracted_fields: Record<string, string>; extraction_status: ArchiveExtractionStatus; error_message?: string; mirror_status?: ArchiveMirrorStatus; mirror_error_message?: string; archived_at?: string; trashed_at?: string; created_at: string; updated_at: string; customer?: ArchiveCustomer; links?: Array<{ id: string; from_document_id: string; to_document_id: string; relation: string; link_status: string }>; evidence?: ArchiveEvidence[] }
export interface ArchiveBatch { id: string; total: number; completed: number; failed: number; status: string; created_at: string; updated_at: string }
export interface ArchiveReminder { id: string; document_id?: string; customer_id?: string; assignee_id: string; type: string; title: string; description: string; rule: Record<string, unknown>; status: ArchiveReminderStatus; confidence: number; due_at?: string; snoozed_until?: string; created_at: string }
export interface ArchiveReminderCandidate { id: string; document_id: string; document_title: string; customer_id?: string; assignee_id?: string; type: string; source_field: string; event_at: string; suggested_offset_days: number; title: string; description: string; confidence: number; quote: string; locator: Record<string, unknown>; rule: Record<string, unknown>; needs_review: boolean; status: 'pending' | 'created' | 'superseded' | 'ignored'; reminder_id?: string; created_at: string; updated_at: string }
export interface ArchiveNotification { id: string; reminder_id?: string; occurrence_id?: string; title: string; body: string; read_at?: string; created_at: string }
export interface ArchiveBulkActionResult { action: ArchiveBulkAction; requested: number; succeeded: number; failed: number; items: Array<{ id: string; success: boolean; error?: string }> }
export interface ArchiveSettings { id: string; managed_knowledge_base_id: string; timezone: string; extraction_model_id: string; extraction_version: string; trash_retention_days: number }
export interface ArchiveSearchResponse { answer: string; documents: ArchiveDocument[]; customers: ArchiveCustomer[]; citations: Array<{ document_id: string; field_name?: string; quote: string; locator: Record<string, unknown> }>; total: number }
export interface ArchiveSearchFilters {
  document_type?: ArchiveDocumentType
  business_type?: string
  customer_id?: string
  model?: string
  serial_number?: string
  agreement_number?: string
  from?: string
  to?: string
  imported_from?: string
  imported_to?: string
  extraction_statuses?: ArchiveExtractionStatus[]
  archived?: boolean
}
export interface ArchiveSearchRequest { query: string; filters: ArchiveSearchFilters; page: number; page_size: number }
export interface ArchiveApiResponse<T> { success: boolean; data: T }

export const getArchiveSettings = () => get<ArchiveApiResponse<ArchiveSettings>>('/api/v1/archive/settings')
export const updateArchiveSettings = (data: Partial<ArchiveSettings>) => patch<ArchiveApiResponse<ArchiveSettings>>('/api/v1/archive/settings', data)
export const listArchiveDocuments = (q = '', archived = false) => get<ArchiveApiResponse<ArchiveDocument[]>>(`/api/v1/archive/documents?q=${encodeURIComponent(q)}&archived=${archived}`)
export const getArchiveBatch = (id: string) => get<ArchiveApiResponse<ArchiveBatch>>(`/api/v1/archive/import-batches/${id}`)
export const getArchiveDocument = (id: string) => get<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}`)
export const updateArchiveDocument = (id: string, data: Record<string, unknown>) => patch<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}`, data)
export const retryArchiveDocumentExtraction = (id: string) => post<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}/retry-extraction`)
export const cancelArchiveDocumentExtraction = (id: string) => post<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}/cancel-extraction`)
export const retryArchiveDocumentMirror = (id: string) => post<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}/retry-mirror`)
export const archiveDocument = (id: string) => post<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}/archive`)
export const restoreDocument = (id: string) => post<ArchiveApiResponse<ArchiveDocument>>(`/api/v1/archive/documents/${id}/restore`)
export const bulkArchiveDocuments = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/documents/bulk/archive', { ids })
export const bulkRestoreDocuments = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/documents/bulk/restore', { ids })
export const deleteArchiveDocument = (id: string) => del(`/api/v1/archive/documents/${id}`)
export const bulkDeleteArchiveDocuments = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/documents/bulk/delete', { ids })
export const bulkPurgeArchiveDocuments = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/documents/bulk/purge', { ids })
export const getArchiveDocumentEvidence = (id: string) => get<ArchiveApiResponse<ArchiveEvidence[]>>(`/api/v1/archive/documents/${id}/evidence`)
export const getArchiveDocumentPreview = (id: string) => getDown(`/api/v1/archive/documents/${id}/preview`)
export const listArchiveCustomers = (q = '') => get<ArchiveApiResponse<ArchiveCustomer[]>>(`/api/v1/archive/customers?q=${encodeURIComponent(q)}`)
export const updateArchiveCustomer = (id: string, data: Record<string, unknown>) => patch<ArchiveApiResponse<ArchiveCustomer>>(`/api/v1/archive/customers/${id}`, data)
export const searchArchive = (data: ArchiveSearchRequest) => post<ArchiveApiResponse<ArchiveSearchResponse>>('/api/v1/archive/search', data)
export const listArchiveReminders = (status = '') => get<ArchiveApiResponse<ArchiveReminder[]>>(`/api/v1/archive/reminders${status ? `?status=${encodeURIComponent(status)}` : ''}`)
export const listArchiveReminderCandidates = (status = 'pending') => get<ArchiveApiResponse<ArchiveReminderCandidate[]>>(`/api/v1/archive/reminder-candidates${status ? `?status=${encodeURIComponent(status)}` : ''}`)
export const bulkIgnoreArchiveReminderCandidates = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/reminder-candidates/bulk/ignore', { ids })
export const createArchiveReminderFromCandidate = (id: string, data: { offset_days: number; time: string; assignee_id?: string }) => post<ArchiveApiResponse<ArchiveReminder>>(`/api/v1/archive/reminder-candidates/${id}/create`, data)
export const createArchiveReminder = (data: Partial<ArchiveReminder>) => post<ArchiveApiResponse<ArchiveReminder>>('/api/v1/archive/reminders', data)
export const updateArchiveReminder = (id: string, data: Record<string, unknown>) => patch<ArchiveApiResponse<ArchiveReminder>>(`/api/v1/archive/reminders/${id}`, data)
export const deleteArchiveReminder = (id: string) => del(`/api/v1/archive/reminders/${id}`)
export const bulkDeleteArchiveReminders = (ids: string[]) => post<ArchiveApiResponse<ArchiveBulkActionResult>>('/api/v1/archive/reminders/bulk/delete', { ids })
export const listArchiveNotifications = (unread = false) => get<ArchiveApiResponse<ArchiveNotification[]>>(`/api/v1/archive/notifications?unread=${unread}`)
export const markArchiveNotificationRead = (id: string) => post(`/api/v1/archive/notifications/${id}/read`)
export const deleteArchiveNotification = (id: string) => del(`/api/v1/archive/notifications/${id}`)

export const importArchiveFiles = (files: File[], onProgress?: (value: number) => void) => {
  const data = new FormData(); files.forEach((file) => data.append('files', file))
  return postUpload('/api/v1/archive/import-batches', data, (event) => { if (event.total) onProgress?.(Math.round((event.loaded / event.total) * 100)) }) as Promise<ArchiveApiResponse<ArchiveBatch>>
}

export function streamArchiveBatch(id: string, signal: AbortSignal, onProgress: (batch: ArchiveBatch) => void) {
  const token = localStorage.getItem('weknora_token') || ''; const tenant = localStorage.getItem('weknora_selected_tenant_id') || ''
  return fetchEventSource(`${getApiBaseUrl()}/api/v1/archive/import-batches/${id}/events`, { signal, openWhenHidden: true, headers: { Authorization: `Bearer ${token}`, 'Accept-Language': localStorage.getItem('locale') || 'zh-CN', ...(tenant ? { 'X-Tenant-ID': tenant } : {}) }, async onopen(response) { if (!response.ok) throw new Error(`HTTP ${response.status}`) }, onmessage(event) { if (event.event === 'progress' && event.data) { try { onProgress(JSON.parse(event.data) as ArchiveBatch) } catch { /* Ignore a malformed progress frame; polling remains the source of truth. */ } } }, onerror() { if (!signal.aborted) return 1500 } })
}
