import type { ContractReviewBulkResult, ReviewStatus } from '@/api/contract-review'

/** Return the IDs that the server confirmed as successful in a bulk response. */
export function successfulBulkIds(result: Pick<ContractReviewBulkResult, 'items'>): Set<string> {
  return new Set(result.items.filter((item) => item.success).map((item) => item.id))
}

/** Keep failed bulk rows visible so the UI can report partial success accurately. */
export function removeSuccessfulRows<T extends { id: string }>(
  rows: T[],
  result: Pick<ContractReviewBulkResult, 'items'>,
): T[] {
  const succeeded = successfulBulkIds(result)
  return rows.filter((row) => !succeeded.has(row.id))
}

export function contractReviewIsSettled(status: ReviewStatus): boolean {
  return status === 'completed' || status === 'failed' || status === 'cancelled' || status === 'ready'
}
