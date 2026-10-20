import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  TRAFFIC_SAMPLE_KEY,
  TRAFFIC_SAMPLE_MAX_AGE_MS,
  readTrafficSample,
  writeTrafficSample,
} from './traffic-sample.ts'

const at = Date.parse('2024-05-01T12:00:00.000Z')

const reading = (over = {}) => ({
  enabled: true,
  counting: true,
  usage: [
    { address: '192.168.1.5', exit: '', unknown: false, up_bytes: 10, down_bytes: 20, up_packets: 1, down_packets: 2 },
    { address: '192.168.1.6', exit: 'Proxy', up_bytes: 30, down_bytes: 40, up_packets: 3, down_packets: 4 },
  ],
  held: 2,
  capacity: 8192,
  as_of: '2024-05-01T12:00:00.000Z',
  ...over,
})

class MemoryStore {
  #items = new Map()
  getItem(key) { return this.#items.get(key) ?? null }
  setItem(key, value) { this.#items.set(key, String(value)) }
}

test('a reading comes back inside the window, trimmed to what a difference needs', () => {
  const store = new MemoryStore()
  writeTrafficSample(reading(), store)
  assert.deepEqual(readTrafficSample(at + 30_000, store), {
    as_of: '2024-05-01T12:00:00.000Z',
    counting: true,
    usage: [
      { address: '192.168.1.5', exit: '', up_bytes: 10, down_bytes: 20 },
      { address: '192.168.1.6', exit: 'Proxy', up_bytes: 30, down_bytes: 40 },
    ],
  })
})

test('a sample at the edge of the window still counts; a second later it does not', () => {
  const store = new MemoryStore()
  writeTrafficSample(reading(), store)
  assert.ok(readTrafficSample(at + TRAFFIC_SAMPLE_MAX_AGE_MS, store))
  assert.equal(readTrafficSample(at + TRAFFIC_SAMPLE_MAX_AGE_MS + 1, store), undefined)
})

test('counters that are off are not kept, because they carry no rate', () => {
  const store = new MemoryStore()
  writeTrafficSample(reading({ counting: false }), store)
  assert.equal(store.getItem(TRAFFIC_SAMPLE_KEY), null)
})

test('a damaged or foreign saved reading is ignored rather than thrown', () => {
  const store = new MemoryStore()
  for (const value of [
    'not json',
    'null',
    '{"as_of":"not a time","counting":true,"usage":[]}',
    '{"as_of":"2024-05-01T12:00:00.000Z","counting":true}',
    '{"as_of":"2024-05-01T12:00:00.000Z","counting":true,"usage":{}}',
  ]) {
    store.setItem(TRAFFIC_SAMPLE_KEY, value)
    assert.equal(readTrafficSample(at + 1000, store), undefined, value)
  }
})

test('a missing store is the same as no saved reading', () => {
  assert.equal(readTrafficSample(at, undefined), undefined)
  assert.doesNotThrow(() => writeTrafficSample(reading(), undefined))
})
