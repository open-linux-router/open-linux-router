import assert from 'node:assert/strict'
import { test } from 'node:test'

import { linkTestReading, linkTestResult } from './link-test.ts'

/** One finished sample, as the LibreSpeed worker reports it. */
const finished = {
  testState: 4,
  dlStatus: '321.45',
  ulStatus: '98.6',
  pingStatus: '4.2',
  jitterStatus: '0.8',
  dlProgress: 1,
  ulProgress: 1,
  pingProgress: 1,
}

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

test('a dial reads nothing until its direction has started', () => {
  const notStarted = { testState: 0, dlStatus: '0', ulStatus: '0', pingStatus: '0', jitterStatus: '0', dlProgress: 0, ulProgress: 0, pingProgress: 0 }
  assert.equal(linkTestReading(notStarted, 'download'), null)
  assert.equal(linkTestReading(notStarted, 'upload'), null)
  assert.equal(linkTestReading(notStarted, 'latency'), null)
  assert.equal(linkTestReading(notStarted, 'jitter'), null)
  assert.equal(linkTestReading(null, 'download'), null)
})

test('a dial follows the direction being measured and holds the other', () => {
  const downloading = { ...finished, testState: 1, dlStatus: '212.5', dlProgress: 0.4, ulStatus: '0', ulProgress: 0 }
  assert.equal(linkTestReading(downloading, 'download'), 212.5)
  assert.equal(linkTestReading(downloading, 'upload'), null)
  assert.equal(linkTestReading(finished, 'download'), 321.45)
})

test('a failed direction never becomes a zero reading', () => {
  assert.equal(linkTestReading({ ...finished, dlStatus: 'Fail' }, 'download'), null)
})

test('a direction that has started may read zero', () => {
  assert.equal(linkTestReading({ ...finished, ulStatus: '0', ulProgress: 0.3 }, 'upload'), 0)
})

test('a measured zero jitter is a reading, not a missing value', () => {
  assert.equal(linkTestReading({ ...finished, jitterStatus: '0' }, 'jitter'), 0)
})
