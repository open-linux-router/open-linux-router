import assert from 'node:assert/strict'
import { test } from 'node:test'
import { activityFloor, activityLevel, activityThresholds } from './activity-scale.ts'

test('absolute gray tiers stay separate from fractions of the maximum', () => {
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
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor])), 5)
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor, activityFloor])), 5)
  assert.equal(activityLevel(20 * activityFloor, [20 * activityFloor, 30 * activityFloor, 40 * activityFloor]), 2)
})

test('sample counts do not change maximum-based boundaries', () => {
  const unit = activityFloor
  const thresholds = activityThresholds([unit, unit, unit, unit, 100 * unit])
  assert.deepEqual(thresholds, [25 * unit, 50 * unit, 75 * unit])
  assert.deepEqual([25, 50, 75, 100].map(value => activityLevel(value * unit, thresholds)), [2, 3, 4, 5])
  assert.equal(activityLevel(25 * unit + 1, thresholds), 3)
  assert.equal(activityLevel(50 * unit + 1, thresholds), 4)
  assert.equal(activityLevel(75 * unit + 1, thresholds), 5)
})
