import type { ArchiveDocument, ArchiveExtractionStatus, ArchiveSearchFilters } from '@/api/smart-archive'

export interface ArchiveDocumentFilterState {
  dateFrom: string
  dateTo: string
  documentType: string
  statuses: ArchiveExtractionStatus[]
  archived: boolean
}

function localDateBoundary(value: string, endOfDay: boolean): string | undefined {
  if (!value) return undefined
  const suffix = endOfDay ? 'T23:59:59.999' : 'T00:00:00.000'
  const date = new Date(`${value}${suffix}`)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

export function buildArchiveSearchFilters(state: ArchiveDocumentFilterState): ArchiveSearchFilters {
  return {
    ...(state.documentType ? { document_type: state.documentType as ArchiveSearchFilters['document_type'] } : {}),
    ...(state.dateFrom ? { imported_from: localDateBoundary(state.dateFrom, false) } : {}),
    ...(state.dateTo ? { imported_to: localDateBoundary(state.dateTo, true) } : {}),
    ...(state.statuses.length ? { extraction_statuses: state.statuses } : {}),
    archived: state.archived,
  }
}

export function mergeArchiveDocuments(existing: ArchiveDocument[], incoming: ArchiveDocument[]): ArchiveDocument[] {
  const rows = new Map(existing.map(document => [document.id, document]))
  incoming.forEach(document => rows.set(document.id, document))
  return [...rows.values()]
}

export function hasMoreArchiveDocuments(documents: ArchiveDocument[], total: number): boolean {
  return documents.length < total
}

export function archiveDocumentStatusTone(status: ArchiveExtractionStatus): 'queued' | 'running' | 'completed' | 'failed' | 'review' {
  if (status === 'uploading') return 'queued'
  if (status === 'completed') return 'completed'
  if (status === 'failed') return 'failed'
  if (status === 'needs_review') return 'review'
  return 'running'
}
