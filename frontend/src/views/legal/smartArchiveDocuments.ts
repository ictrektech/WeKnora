import type { ArchiveDocument, ArchiveExtractionStatus, ArchiveMirrorStatus, ArchiveSearchFilters } from '@/api/smart-archive'

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

export function archiveDocumentStatusTone(status: ArchiveExtractionStatus): 'queued' | 'running' | 'completed' | 'failed' | 'review' | 'canceled' {
  if (status === 'uploading') return 'queued'
  if (status === 'completed') return 'completed'
  if (status === 'failed') return 'failed'
  if (status === 'canceled') return 'canceled'
  if (status === 'needs_review') return 'review'
  return 'running'
}

export interface ArchiveDocumentDisplayStatus {
  status: ArchiveExtractionStatus | ArchiveMirrorStatus
  source: 'extraction' | 'mirror'
  tone: 'queued' | 'running' | 'completed' | 'failed' | 'review' | 'canceled'
}

/**
 * A document has two backend states (extraction and knowledge-base mirror),
 * but the list should expose one status that answers "can I use it now?".
 * Once extraction is complete, mirror work becomes the only relevant state;
 * a submitted mirror is therefore represented by the single final "completed"
 * state instead of showing two successful labels side by side.
 */
export function archiveDocumentDisplayStatus(document: Pick<ArchiveDocument, 'extraction_status' | 'mirror_status'>): ArchiveDocumentDisplayStatus {
  if (document.extraction_status !== 'completed') {
    return { status: document.extraction_status, source: 'extraction', tone: archiveDocumentStatusTone(document.extraction_status) }
  }

  switch (document.mirror_status) {
    case 'not_started':
    case 'pending':
      return { status: 'pending', source: 'mirror', tone: 'review' }
    case 'processing':
      return { status: 'processing', source: 'mirror', tone: 'running' }
    case 'failed':
      return { status: 'failed', source: 'mirror', tone: 'failed' }
    default:
      return { status: 'completed', source: 'extraction', tone: 'completed' }
  }
}
