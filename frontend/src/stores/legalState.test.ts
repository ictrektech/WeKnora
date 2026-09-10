import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  archiveDocumentIsProcessing,
  archiveImportProgress,
  contractReviewIsSettled,
  removeSuccessfulRows,
} from './legalState.ts'

test('bulk state keeps failed contract review rows visible', () => {
  const rows = [{ id: 'review-1' }, { id: 'review-2' }]
  const result = {
    items: [
      { id: 'review-1', success: true },
      { id: 'review-2', success: false, error: 'running' },
    ],
  }

  assert.deepEqual(removeSuccessfulRows(rows, result), [{ id: 'review-2' }])
})

test('settled states stop live review updates', () => {
  assert.equal(contractReviewIsSettled('ready'), true)
  assert.equal(contractReviewIsSettled('completed'), true)
  assert.equal(contractReviewIsSettled('failed'), true)
  assert.equal(contractReviewIsSettled('cancelled'), true)
  assert.equal(contractReviewIsSettled('analyzing'), false)
})

test('archive processing states keep the fallback poll active', () => {
  assert.equal(archiveDocumentIsProcessing('uploading'), true)
  assert.equal(archiveDocumentIsProcessing('parsing'), true)
  assert.equal(archiveDocumentIsProcessing('extracting'), true)
  assert.equal(archiveDocumentIsProcessing('linking'), true)
  assert.equal(archiveDocumentIsProcessing('completed'), false)
  assert.equal(archiveDocumentIsProcessing('failed'), false)
})

test('archive import progress combines completed and failed files', () => {
  assert.equal(archiveImportProgress({ total: 4, completed: 1, failed: 1 }), 50)
  assert.equal(archiveImportProgress({ total: 4, completed: 4, failed: 1 }), 100)
  assert.equal(archiveImportProgress({ total: 0, completed: 0, failed: 0 }), 0)
})
