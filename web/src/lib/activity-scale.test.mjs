import assert from 'node:assert/strict'
import { test } from 'node:test'
import { activityFloor, activityLevel, activityThresholds } from './activity-scale.ts'

test('absolute gray tiers stay separate from relative green quartiles', () => {
  const values = [10, 20, 30, 40].map(value => value * 1024 ** 2)
  const thresholds = activityThresholds([0, 1, activityFloor - 1, ...values])
  assert.equal(activityLevel(0, thresholds), 0)
  assert.equal(activityLevel(activityFloor - 1, thresholds), 1)
  assert.deepEqual(values.map(value => activityLevel(value, thresholds)), [2, 3, 4, 5])
  assert.deepEqual(activityThresholds([]), [])
})

test('relative scale follows device magnitude and keeps equal totals together', () => {
  for (const multiplier of [1, 1000]) {
    const values = [10, 20, 30, 40].map(value => value * activityFloor * multiplier)
    assert.deepEqual(values.map(value => activityLevel(value, activityThresholds(values))), [2, 3, 4, 5])
  }
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor])), 2)
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor, activityFloor])), 2)
  assert.equal(activityLevel(20 * activityFloor, [20 * activityFloor, 30 * activityFloor, 40 * activityFloor]), 2)
})
