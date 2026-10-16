import assert from 'node:assert/strict'
import { test } from 'node:test'
import { activityFloor, activityLevel, activityThresholds } from './activity-scale.ts'

test('absolute gray tiers stay separate from fractions of the maximum', () => {
  const values = [256, 512, 768, 1024].map(value => value * 1024 ** 2)
  const thresholds = activityThresholds([0, 1, activityFloor - 1, ...values])
  assert.equal(activityLevel(0, thresholds), 0)
  assert.equal(activityLevel(activityFloor - 1, thresholds), 1)
  assert.deepEqual(values.map(value => activityLevel(value, thresholds)), [2, 3, 4, 5])
  assert.deepEqual(activityThresholds([]), [])
})

test('relative scale follows device magnitude and keeps equal totals together', () => {
  for (const multiplier of [10, 1000]) {
    const values = [10, 20, 30, 40].map(value => value * activityFloor * multiplier)
    assert.deepEqual(values.map(value => activityLevel(value, activityThresholds(values))), [2, 3, 4, 5])
  }
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor])), 2)
  assert.equal(activityLevel(activityFloor, activityThresholds([activityFloor, activityFloor])), 2)
  assert.equal(activityLevel(20 * activityFloor, [20 * activityFloor, 30 * activityFloor, 40 * activityFloor]), 2)
})

test('sample counts do not change maximum-based boundaries', () => {
  const unit = 1024 ** 3
  const thresholds = activityThresholds([unit, unit, unit, unit, 100 * unit])
  assert.deepEqual(thresholds, [25 * unit, 50 * unit, 75 * unit])
  assert.deepEqual([25, 50, 75, 100].map(value => activityLevel(value * unit, thresholds)), [2, 3, 4, 5])
  assert.equal(activityLevel(25 * unit + 1, thresholds), 3)
  assert.equal(activityLevel(50 * unit + 1, thresholds), 4)
  assert.equal(activityLevel(75 * unit + 1, thresholds), 5)
})

test('small histories use a 1 GiB reference with lighter boundary tiers', () => {
  const mib = 1024 ** 2
  const thresholds = activityThresholds([11.6 * mib + 620.2 * 1024])
  assert.deepEqual(thresholds, [256, 512, 768].map(value => value * mib))
  assert.equal(activityLevel(11.6 * mib + 620.2 * 1024, thresholds), 2)
  for (const [index, boundary] of thresholds.entries()) {
    assert.equal(activityLevel(boundary, thresholds), 2 + index)
    assert.equal(activityLevel(boundary + 1, thresholds), 3 + index)
  }
  assert.deepEqual(activityThresholds([1024 * mib]), thresholds)
})
