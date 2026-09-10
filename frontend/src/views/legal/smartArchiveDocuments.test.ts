import assert from 'node:assert/strict'
import { test } from 'node:test'

import type { ArchiveDocument } from '@/api/smart-archive'
import {
  archiveDocumentStatusTone,
  buildArchiveSearchFilters,
  hasMoreArchiveDocuments,
  mergeArchiveDocuments,
} from './smartArchiveDocuments.ts'

function document(id: string, title = id): ArchiveDocument {
  return {
    id,
    title,
    file_name: `${id}.pdf`,
    file_type: 'pdf',
    file_size: 100,
    document_type: 'contract',
    business_type: '',
    agreement_number: '',
    amount: 0,
    currency: '',
    extracted_fields: {},
    extraction_status: 'completed',
    created_at: '2026-08-19T00:00:00Z',
    updated_at: '2026-08-19T00:00:00Z',
  }
}

test('builds archive search filters with import-day boundaries', () => {
  const filters = buildArchiveSearchFilters({
    dateFrom: '2026-08-19',
    dateTo: '2026-08-20',
    documentType: 'contract',
    statuses: ['completed', 'failed'],
    archived: false,
  })

  assert.equal(filters.document_type, 'contract')
  assert.equal(filters.archived, false)
  assert.deepEqual(filters.extraction_statuses, ['completed', 'failed'])
  assert.ok(filters.imported_from?.includes('T'))
  assert.ok(filters.imported_to?.includes('T'))
})

test('merges loaded pages by document id and keeps newer snapshots', () => {
  const merged = mergeArchiveDocuments(
    [document('one'), document('two', 'old')],
    [document('two', 'new'), document('three')],
  )

  assert.deepEqual(merged.map(item => item.id), ['one', 'two', 'three'])
  assert.equal(merged[1].title, 'new')
  assert.equal(hasMoreArchiveDocuments(merged, 4), true)
  assert.equal(hasMoreArchiveDocuments(merged, 3), false)
})

test('maps extraction states to stable status tones', () => {
  assert.equal(archiveDocumentStatusTone('uploading'), 'queued')
  assert.equal(archiveDocumentStatusTone('parsing'), 'running')
  assert.equal(archiveDocumentStatusTone('extracting'), 'running')
  assert.equal(archiveDocumentStatusTone('linking'), 'running')
  assert.equal(archiveDocumentStatusTone('completed'), 'completed')
  assert.equal(archiveDocumentStatusTone('failed'), 'failed')
  assert.equal(archiveDocumentStatusTone('needs_review'), 'review')
})
