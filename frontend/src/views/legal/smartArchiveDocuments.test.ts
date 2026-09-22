import assert from 'node:assert/strict'
import { test } from 'node:test'

import type { ArchiveDocument } from '@/api/smart-archive'
import {
  archiveDocumentStatusTone,
  archiveDocumentDisplayStatus,
  archiveDocumentStages,
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

test('maps persisted milestones to one document stage sequence', () => {
  const states = (extraction_status: ArchiveDocument['extraction_status'], extraction_progress: number) => archiveDocumentStages({ extraction_status, extraction_progress }).map(stage => stage.state)

  assert.deepEqual(states('parsing', 5), ['active', 'pending', 'pending', 'pending', 'pending'])
  assert.deepEqual(states('parsing', 10), ['completed', 'active', 'pending', 'pending', 'pending'])
  assert.deepEqual(states('extracting', 45), ['completed', 'completed', 'active', 'pending', 'pending'])
  assert.deepEqual(states('linking', 75), ['completed', 'completed', 'completed', 'active', 'pending'])
  assert.deepEqual(states('linking', 90), ['completed', 'completed', 'completed', 'completed', 'active'])
  assert.deepEqual(states('completed', 100), ['completed', 'completed', 'completed', 'completed', 'completed'])
  assert.deepEqual(states('failed', 100), ['completed', 'completed', 'completed', 'completed', 'failed'])
  assert.deepEqual(states('needs_review', 100), ['completed', 'completed', 'completed', 'completed', 'needs_review'])
  assert.deepEqual(states('needs_review', 10), ['completed', 'needs_review', 'pending', 'pending', 'pending'])
  assert.deepEqual(states('canceled', 75), ['completed', 'completed', 'completed', 'canceled', 'pending'])
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
