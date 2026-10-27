import assert from 'node:assert/strict'
import { test } from 'node:test'

import { linkTestResult } from './link-test.ts'

/** One finished sample, as the LibreSpeed worker reports it. */
const finished = { testState: 4, dlStatus: '321.45', ulStatus: '98.6', pingStatus: '4.2', jitterStatus: '0.8' }

test('a finished sample reports the settled numbers', () => {
  assert.deepEqual(linkTestResult(finished), { latency: 4.2, download: 321.45, upload: 98.6, jitter: 0.8 })
})

test('unmeasured jitter stays null rather than becoming zero', () => {
  assert.deepEqual(linkTestResult({ ...finished, jitterStatus: '' }), { latency: 4.2, download: 321.45, upload: 98.6, jitter: null })
})

test('a direction that failed is not a result', () => {
  assert.equal(linkTestResult({ ...finished, dlStatus: 'Fail' }), null)
  assert.equal(linkTestResult({ ...finished, dlStatus: '0' }), null)
  assert.equal(linkTestResult({ ...finished, ulStatus: 'Fail' }), null)
})

test('an unmeasured latency is not a result', () => {
  assert.equal(linkTestResult({ ...finished, pingStatus: '' }), null)
  assert.equal(linkTestResult({ ...finished, pingStatus: '-1' }), null)
})
