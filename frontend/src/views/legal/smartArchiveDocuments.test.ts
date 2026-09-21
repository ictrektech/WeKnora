import assert from 'node:assert/strict'
import { test } from 'node:test'

import type { ArchiveDocument } from '@/api/smart-archive'
import {
  archiveDocumentStatusTone,
  archiveDocumentDisplayStatus,
  archiveDocumentProgress,
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
    extraction_progress: 100,
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
  assert.equal(archiveDocumentStatusTone('canceled'), 'canceled')
  assert.equal(archiveDocumentStatusTone('needs_review'), 'review')
})

test('uses persisted progress for one archive document', () => {
  assert.equal(archiveDocumentProgress({ extraction_status: 'parsing', extraction_progress: 10 }), 10)
  assert.equal(archiveDocumentProgress({ extraction_status: 'extracting', extraction_progress: 45 }), 45)
  assert.equal(archiveDocumentProgress({ extraction_status: 'linking', extraction_progress: 75 }), 75)
  assert.equal(archiveDocumentProgress({ extraction_status: 'completed', extraction_progress: 0 }), 100)
  assert.equal(archiveDocumentProgress({ extraction_status: 'needs_review', extraction_progress: 10 }), 10)
  assert.equal(archiveDocumentProgress({ extraction_status: 'failed', extraction_progress: 130 }), 100)
})

test('exposes one user-facing status across extraction and mirror stages', () => {
  assert.deepEqual(archiveDocumentDisplayStatus({ extraction_status: 'completed', mirror_status: 'submitted' }), {
    status: 'completed',
    source: 'extraction',
    tone: 'completed',
  })
  assert.deepEqual(archiveDocumentDisplayStatus({ extraction_status: 'completed', mirror_status: 'processing' }), {
    status: 'processing',
    source: 'mirror',
    tone: 'running',
  })
  assert.deepEqual(archiveDocumentDisplayStatus({ extraction_status: 'completed', mirror_status: 'not_started' }), {
    status: 'pending',
    source: 'mirror',
    tone: 'review',
  })
  assert.deepEqual(archiveDocumentDisplayStatus({ extraction_status: 'needs_review', mirror_status: 'submitted' }), {
    status: 'needs_review',
    source: 'extraction',
    tone: 'review',
  })
  assert.deepEqual(archiveDocumentDisplayStatus({ extraction_status: 'canceled', mirror_status: 'not_started' }), {
    status: 'canceled',
    source: 'extraction',
    tone: 'canceled',
  })
})
