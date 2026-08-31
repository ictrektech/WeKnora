import assert from 'node:assert/strict'
import { test } from 'node:test'

import { contractReviewIsSettled, removeSuccessfulRows } from './legalState.ts'

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
  assert.equal(contractReviewIsSettled('analyzing'), false)
})
