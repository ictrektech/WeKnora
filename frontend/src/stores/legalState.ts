import type { ArchiveBatch, ArchiveExtractionStatus } from '@/api/smart-archive'
import type { ReviewStatus } from '@/api/contract-review'

export interface BulkActionItems {
  items: Array<{ id: string; success: boolean; error?: string }>
}

/** Return the IDs that the server confirmed as successful in a bulk response. */
export function successfulBulkIds(result: Pick<BulkActionItems, 'items'>): Set<string> {
  return new Set(result.items.filter((item) => item.success).map((item) => item.id))
}

/** Keep failed bulk rows visible so the UI can report partial success accurately. */
export function removeSuccessfulRows<T extends { id: string }>(
  rows: T[],
  result: Pick<BulkActionItems, 'items'>,
): T[] {
  const succeeded = successfulBulkIds(result)
  return rows.filter((row) => !succeeded.has(row.id))
}

export function contractReviewIsSettled(status: ReviewStatus): boolean {
  return status === 'completed' || status === 'failed' || status === 'cancelled' || status === 'ready'
}

export function archiveDocumentIsProcessing(status: ArchiveExtractionStatus): boolean {
  return status === 'uploading' || status === 'parsing' || status === 'extracting' || status === 'linking'
}

export function archiveImportProgress(batch: Pick<ArchiveBatch, 'total' | 'completed' | 'failed'>): number {
  if (batch.total <= 0) return 0
  return Math.min(100, Math.round(((batch.completed + batch.failed) / batch.total) * 100))
}
