import { fetchEventSource } from '@microsoft/fetch-event-source'

import { getApiBaseUrl } from '@/utils/api-base'
import { del, get, patch, post, postUpload } from '@/utils/request'

export type ReviewStatus = 'draft' | 'uploading' | 'ready' | 'analyzing' | 'reviewing_clauses' | 'completed' | 'failed' | 'cancelled'
export type RiskLevel = 'high' | 'medium' | 'low'
export type RepresentedParty = 'customer' | 'vendor' | 'neutral'
export type EvidenceStatus = 'pending' | 'located' | 'legacy_exact' | 'multiple_matches' | 'not_found' | 'version_mismatch' | 'unsupported' | 'error'
export type LocatorLoadStatus = 'idle' | 'loading' | 'ready' | 'unsupported' | 'error'
export type ConfidenceLevel = 'high' | 'medium' | 'low' | 'unknown'
export type ReviewQualityStatus = 'pending' | 'valid' | 'degraded' | 'invalid' | 'stale' | 'legacy'

// These are intentionally open string unions. A playbook may add a category
// or finding type before the UI ships a new translation; the fallback label
// keeps the result readable without rejecting an otherwise valid response.
export type ReviewCategory = string
export type ReviewFindingType = string

export interface ReviewEvidence {
  source_start?: number
  source_end?: number
  source_revision?: string
  source_text_hash?: string
  unit_ids?: string[]
  page_start?: number
  page_end?: number
  status?: EvidenceStatus
}

export interface ReviewFact {
  type?: string
  key?: string
  label?: string
  value?: string
  text?: string
  normalized_value?: string
  unit?: string
  currency?: string
  condition?: string
  status?: string
  evidence_quote?: string
  evidence_refs?: string[]
  source_start?: number
  source_end?: number
}

export type ReviewFactValue = string | ReviewFact

export interface ReviewWarning {
  code?: string
  message?: string
  clause_id?: string
  evidence_id?: string
}

export type ReviewWarningValue = string | ReviewWarning

export interface ContractReviewLocatorUnit {
  unit_id: string
  kind?: string
  parent_id?: string
  page?: number
  source_start: number
  source_end: number
  text?: string
  // Optional rendered offsets let the viewer disambiguate repeated text
  // without guessing. They are normalized document offsets, end-exclusive.
  rendered_start?: number
  rendered_end?: number
}

export interface ContractReviewLocator {
  version?: number
  offset_unit?: string
  source_hash?: string
  source_length?: number
  review_id?: string
  source_revision?: string
  source_text_hash?: string
  // `text` is the canonical extracted source text in newer responses.
  // `source_text` is accepted as an explicit alias for compatibility.
  text?: string
  source_text?: string
  units?: ContractReviewLocatorUnit[]
  source_units_json?: ContractReviewLocatorUnit[] | string
}

export interface ReviewClause {
  id: string
  review_id: string
  sequence: number
  title: string
  excerpt: string
  source_start: number
  source_end: number
  source_revision?: string
  review_status: string
  issue_count: number
}

export interface ReviewIssue {
  id: string
  review_id: string
  clause_id: string
  sequence: number
  risk_level: RiskLevel
  title: string
  explanation: string
  original_quote: string
  suggestion: string
  source_start: number
  source_end: number
  source_revision?: string
  evidence?: ReviewEvidence | ReviewEvidence[]
  category?: ReviewCategory
  finding_type?: ReviewFindingType
  evidence_refs?: string[]
  evidence_status?: EvidenceStatus
  confidence?: ConfidenceLevel
  confidence_score?: number
}

export interface ReviewOverview {
  overall_risk?: RiskLevel
  executive_summary?: string
  contract_type?: string
  parties?: string[]
  facts?: ReviewFactValue[]
  quality_status?: ReviewQualityStatus
  warnings?: ReviewWarningValue[]
  key_recommendations?: string[]
  risk_counts?: Record<RiskLevel, number>
}

export interface ReviewPlaybook { id: string; name: string; description: string; version: string }
export type ContractReviewBulkAction = 'archive' | 'restore' | 'delete'
export interface ContractReviewBulkItem { id: string; success: boolean; error?: string }
export interface ContractReviewBulkResult {
  action: ContractReviewBulkAction
  requested: number
  succeeded: number
  failed: number
  items: ContractReviewBulkItem[]
}

export interface ContractReview {
  id: string
  title: string
  title_customized: boolean
  status: ReviewStatus
  progress: number
  playbook_id: string
  playbook_version: string
  represented_party: RepresentedParty
  model_id: string
  file_name: string
  file_type: '.pdf' | '.docx' | ''
  mime_type: string
  file_size: number
  metadata: Record<string, string>
  overview: ReviewOverview
  source_revision?: string
  source_hash?: string
  source_text_hash?: string
  locator?: ContractReviewLocator
  facts?: ReviewFactValue[]
  quality_status?: ReviewQualityStatus
  warnings?: ReviewWarningValue[]
  error_message?: string
  archived_at?: string
  started_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
  clauses?: ReviewClause[]
  issues?: ReviewIssue[]
}

export interface ApiResponse<T> { success: boolean; data: T }

export const listContractReviews = (archived = false) => get<ApiResponse<ContractReview[]>>(`/api/v1/contract-reviews?archived=${archived}`)
export const listContractReviewPlaybooks = () => get<ApiResponse<ReviewPlaybook[]>>('/api/v1/contract-review-playbooks')
export const createContractReview = () => post<ApiResponse<ContractReview>>('/api/v1/contract-reviews')
export const getContractReview = (id: string) => get<ApiResponse<ContractReview>>(`/api/v1/contract-reviews/${id}`)
export const updateContractReview = (id: string, data: Partial<Pick<ContractReview, 'title' | 'playbook_id' | 'represented_party' | 'model_id'>> & { archived?: boolean }) => patch<ApiResponse<ContractReview>>(`/api/v1/contract-reviews/${id}`, data)
export const deleteContractReview = (id: string) => del(`/api/v1/contract-reviews/${id}`)
export const bulkContractReviews = (action: ContractReviewBulkAction, ids: string[]) =>
  post<ApiResponse<ContractReviewBulkResult>>(`/api/v1/contract-reviews/bulk/${action}`, { ids })
export const startContractReview = (id: string) => post<ApiResponse<ContractReview>>(`/api/v1/contract-reviews/${id}/start`)
export const retryContractReview = (id: string) => post<ApiResponse<ContractReview>>(`/api/v1/contract-reviews/${id}/retry`)
export const cancelContractReview = (id: string) => post<ApiResponse<ContractReview>>(`/api/v1/contract-reviews/${id}/cancel`)
export const getContractReviewDocument = (id: string) => get<ArrayBuffer>(`/api/v1/contract-reviews/${id}/document/preview`, { responseType: 'arraybuffer' })
export const getContractReviewLocator = (id: string) =>
  get<ApiResponse<ContractReviewLocator>>(`/api/v1/contract-reviews/${id}/document/locator`)

export const uploadContractReviewDocument = (id: string, file: File, onProgress?: (value: number) => void) => {
  const data = new FormData()
  data.append('file', file)
  return postUpload(`/api/v1/contract-reviews/${id}/document`, data, (event) => {
    if (event.total) onProgress?.(Math.round((event.loaded / event.total) * 100))
  }) as Promise<ApiResponse<ContractReview>>
}

export function streamContractReview(id: string, signal: AbortSignal, onSnapshot: (review: ContractReview) => void) {
  const token = localStorage.getItem('weknora_token') || ''
  const tenant = localStorage.getItem('weknora_selected_tenant_id') || ''
  return fetchEventSource(`${getApiBaseUrl()}/api/v1/contract-reviews/${id}/events`, {
    method: 'GET', signal, openWhenHidden: true,
    headers: {
      Authorization: `Bearer ${token}`,
      'Accept-Language': localStorage.getItem('locale') || 'zh-CN',
      ...(tenant ? { 'X-Tenant-ID': tenant } : {}),
    },
    async onopen(response) { if (!response.ok) throw new Error(`HTTP ${response.status}`) },
    onmessage(event) {
      if (event.event !== 'snapshot' || !event.data) return
      try { onSnapshot(JSON.parse(event.data) as ContractReview) } catch { /* Ignore malformed frames; snapshot polling remains authoritative. */ }
    },
    onerror() { if (!signal.aborted) return 1500 },
  })
}
