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

export type ArchiveDocumentStageId = 'queued' | 'content' | 'fields' | 'linking' | 'finalizing'
export type ArchiveDocumentStageState = 'pending' | 'active' | 'completed' | 'failed' | 'needs_review' | 'canceled'

export interface ArchiveDocumentStage {
  id: ArchiveDocumentStageId
  state: ArchiveDocumentStageState
}

const archiveDocumentStageIds: ArchiveDocumentStageId[] = ['queued', 'content', 'fields', 'linking', 'finalizing']

function archiveDocumentStageIndex(document: Pick<ArchiveDocument, 'extraction_progress' | 'extraction_status'>): number {
  const progress = Number(document.extraction_progress)
  const value = Number.isFinite(progress) ? Math.min(100, Math.max(0, Math.round(progress))) : 0

  switch (document.extraction_status) {
    case 'uploading':
      return 0
    case 'parsing':
      return value >= 10 ? 1 : 0
    case 'extracting':
      return 2
    case 'linking':
      return value >= 90 ? 4 : 3
    case 'completed':
      return 4
    default:
      if (value >= 90) return 4
      if (value >= 75) return 3
      if (value >= 45) return 2
      if (value >= 10) return 1
      return 0
  }
}

export function archiveDocumentStages(document: Pick<ArchiveDocument, 'extraction_progress' | 'extraction_status'>): ArchiveDocumentStage[] {
  const currentIndex = archiveDocumentStageIndex(document)
  const terminalState: ArchiveDocumentStageState | undefined = document.extraction_status === 'failed'
    ? 'failed'
    : document.extraction_status === 'needs_review'
      ? 'needs_review'
      : document.extraction_status === 'canceled'
        ? 'canceled'
        : undefined

  return archiveDocumentStageIds.map((id, index) => ({
    id,
    state: document.extraction_status === 'completed'
      ? 'completed'
      : terminalState && index === currentIndex
        ? terminalState
        : index < currentIndex
          ? 'completed'
          : index === currentIndex
            ? 'active'
            : 'pending',
  }))
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
