import assert from 'node:assert/strict'
import { test } from 'node:test'

import { hourRange, hourlyLatency, latencyDay } from './hourly-latency.ts'

const day = latencyDay('2026-10-30T11:52:00+08:00')
const sample = (hour, value, minute = '30', date = '2026-10-30') => ({
  hour_start: `${date}T${String(hour).padStart(2, '0')}:00:00+08:00`,
  checked_at: `${date}T${String(hour).padStart(2, '0')}:${minute}:00+08:00`,
  milliseconds: value,
})
const site = (samples) => ({ name: 'Example', url: 'https://example.com/', milliseconds: null, checked_at: '', samples })

test('router wall date and offset define the axis independently of browser timezone', () => {
  assert.deepEqual(day, { date: '2026-10-30', hour: 11, zone: 'UTC+08:00' })
  assert.deepEqual(latencyDay('2026-10-30T00:05:00+05:45'), { date: '2026-10-30', hour: 0, zone: 'UTC+05:45' })
  assert.equal(latencyDay('2026-10-30T11:00:00Z').zone, 'UTC')
  assert.equal(hourRange(0), '00:00–01:00')
  assert.equal(hourRange(23), '23:00–24:00')
})

test('all 24 positions remain fixed, with missing and future slots empty', () => {
  const slots = hourlyLatency(site([sample(9, 12), sample(0, 20), sample(11, 50), sample(13, 25)]), day)
  assert.equal(slots.length, 24)
  assert.equal(slots[0].milliseconds, 20)
  assert.equal(slots[1], null)
  assert.equal(slots[9].milliseconds, 12)
  assert.equal(slots[11].milliseconds, 50)
  assert.ok(slots.slice(12).every(value => value === null))
})

test('a manual retest replaces its hour instead of appending or averaging', () => {
  const before = sample(11, 20, '05')
  const after = sample(11, 240, '52')
  for (const samples of [[before, after], [after, before]]) {
    const slots = hourlyLatency(site(samples), day)
    assert.equal(slots[11].milliseconds, 240)
    assert.equal(slots.filter(Boolean).length, 1)
  }
})

test('a failure replaces success while a missing sample stays separate', () => {
  const failed = { ...sample(11, null, '52'), error: 'Timed out' }
  const slots = hourlyLatency(site([sample(11, 20, '05'), failed]), day)
  assert.equal(slots[11].milliseconds, null)
  assert.equal(slots[11].error, 'Timed out')
  assert.equal(slots[10], null)
})

test('midnight removes yesterday without sliding its hours into today', () => {
  const slots = hourlyLatency(site([sample(23, 50, '30', '2026-10-29'), sample(0, 18, '02')]), latencyDay('2026-10-30T00:05:00+08:00'))
  assert.equal(slots[0].milliseconds, 18)
  assert.ok(slots.slice(1).every(value => value === null))
})

test('a probe crossing the hour boundary belongs to its start-hour bucket', () => {
  const crossing = { ...sample(10, 40, '59'), checked_at: '2026-10-30T02:59:59Z' }
  assert.equal(hourlyLatency(site([crossing]), day)[10].milliseconds, 40)
})

test('DST repeated hours keep the later attempt in the same fixed position', () => {
  const first = { hour_start: '2026-11-01T01:00:00-04:00', checked_at: '2026-11-01T05:30:00Z', milliseconds: 20 }
  const second = { hour_start: '2026-11-01T01:00:00-05:00', checked_at: '2026-11-01T06:30:00Z', milliseconds: 40 }
  const slots = hourlyLatency(site([second, first]), latencyDay('2026-11-01T02:05:00-05:00'))
  assert.equal(slots[1].milliseconds, 40)
  assert.equal(slots.filter(Boolean).length, 1)
})

test('invalid timestamps cannot create data, and old daemon outcomes still fit an hour', () => {
  const older = { milliseconds: null, error: 'Timed out', checked_at: '2026-10-30T03:45:00Z' }
  const slots = hourlyLatency(site([{ ...sample(11, 25), checked_at: 'invalid' }, older]), day)
  assert.equal(slots[11], older)
  assert.equal(slots.filter(Boolean).length, 1)
  assert.doesNotThrow(() => latencyDay('invalid', new Date('2026-10-30T00:00:00Z')))
})
