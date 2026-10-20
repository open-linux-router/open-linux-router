import assert from 'node:assert/strict'
import { test } from 'node:test'

import { adminAccessSummary, ddnsSummary, forwardsSummary, ingressSummary, iptvSummary, loading, unavailable } from './summary.ts'

const snapshot = (data, isError = false) => ({ data, isError })
const attention = { text: 'Needs attention', attention: true }

test('port forwards do not claim saved rules are in force when kernel state is unknown', () => {
  const config = snapshot({ enabled: true, forwards: [{ name: 'ssh' }] })
  const status = { enabled: true, known: true }
  assert.equal(forwardsSummary(config, snapshot(status)).text, '1 rule')
  assert.deepEqual(forwardsSummary(config, snapshot({ ...status, known: false })), unavailable)
  assert.deepEqual(forwardsSummary(config, snapshot({ ...status, drifted: true })), attention)
})

test('dynamic DNS keeps the names readable and claims health only from an observation', () => {
  const config = snapshot({ records: [{ name: 'home.example.net' }, { name: 'v6.example.net' }] })
  assert.deepEqual(ddnsSummary(config, snapshot(undefined, true)), { text: 'home.example.net, v6.example.net' })
  assert.deepEqual(ddnsSummary(config, snapshot({ records: [{ name: 'v6.example.net', publish_error: 'no address' }] })), { text: '1 name failing', attention: true })
  assert.deepEqual(ddnsSummary(snapshot({ records: [] }), snapshot({ records: [] })), { text: 'None' })
  assert.deepEqual(ddnsSummary(snapshot(undefined, true), snapshot()), unavailable)
  assert.deepEqual(ddnsSummary(snapshot(), snapshot()), loading)
})

test('IPTV separates saved intent, a stopped proxy and a provider problem', () => {
  const on = snapshot({ enabled: true, networks: ['lan'] })
  assert.deepEqual(iptvSummary(snapshot({ enabled: false }), snapshot()), { text: 'Off' })
  assert.deepEqual(iptvSummary(on, snapshot({ service: { active: true } })), { text: 'On · lan' })
  assert.deepEqual(iptvSummary(snapshot({ enabled: true }), snapshot({ service: { active: true } })), { text: 'On · No LAN' })
  assert.deepEqual(iptvSummary(on, snapshot({ service: { active: false } })), { text: 'Stopped', attention: true })
  assert.deepEqual(iptvSummary(on, snapshot({ service: { active: true }, problem: 'no upstream' })), attention)
  assert.deepEqual(iptvSummary(on, snapshot(undefined, true)), unavailable)
})

test('publishing distinguishes certificate provisioning, renewal failure and service failure', () => {
  const config = snapshot({ enabled: true, services: [{ name: 'nas' }] })
  const status = { enabled: true, service: { active: true }, certificate: { found: true, expires_in_days: 60 } }
  assert.equal(ingressSummary(config, snapshot(status)).text, '1 address')
  assert.equal(ingressSummary(config, snapshot({ ...status, certificate: { found: false } })).text, 'Waiting for certificate')
  for (const certificate of [{ found: true, expires_in_days: 0 }, { found: true, expires_in_days: 20, renewal_overdue: true }]) {
    assert.deepEqual(ingressSummary(config, snapshot({ ...status, certificate })), attention)
  }
  assert.deepEqual(ingressSummary(config, snapshot({ ...status, service: { active: false } })), attention)
  assert.deepEqual(ingressSummary(config, snapshot({ ...status, service: undefined, service_error: 'no D-Bus' })), unavailable)
})

test('administrator access reports the password it has, not a service state', () => {
  assert.deepEqual(adminAccessSummary(snapshot({ password_set: true })), { text: 'Password set' })
  assert.deepEqual(adminAccessSummary(snapshot({ password_set: false })), { text: 'No password set' })
  assert.deepEqual(adminAccessSummary(snapshot(undefined, true)), unavailable)
  assert.deepEqual(adminAccessSummary(snapshot()), loading)
})
