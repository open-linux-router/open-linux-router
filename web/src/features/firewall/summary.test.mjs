import assert from 'node:assert/strict'
import { test } from 'node:test'

import { firewallState, firewallSummary } from './summary.ts'

const rules = { enabled: true, known: true, drifted: false, native: false, blocked_input: 0, blocked_forward: 0 }

test('the directory tile states what the page states', () => {
  assert.deepEqual(firewallState({ ...rules, enabled: false }), { text: 'Off' })
  assert.deepEqual(firewallState({ ...rules, known: false }), { text: 'Status unknown', attention: true })
  assert.deepEqual(firewallState({ ...rules, drifted: true }), { text: 'Rules not verified', attention: true })
  assert.deepEqual(firewallState(rules), { text: 'On' })
  assert.deepEqual(firewallState({ ...rules, native: true }), { text: 'On · native rules' })
  assert.deepEqual(firewallState({ ...rules, native: true, drifted: true }), { text: 'Native rules not verified', attention: true })
})

test('blocked packets belong in the page sentence, not in the tile line', () => {
  const summary = firewallSummary({ ...rules, blocked_input: 3, blocked_forward: 7 })
  assert.equal(summary.headline, 'On')
  assert.match(summary.detail, /10 packets/)
  assert.equal(firewallSummary({ ...rules, native: true }).tone, 'ok')
  assert.equal(firewallSummary({ ...rules, drifted: true }).tone, 'warn')
  assert.equal(firewallState({ ...rules, blocked_input: 3, blocked_forward: 7 }).text, 'On')
})
