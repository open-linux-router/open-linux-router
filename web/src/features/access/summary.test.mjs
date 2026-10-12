import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('./summary.ts', import.meta.url), 'utf8')
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
const { gatewaySummary, socksOutSummary, remoteSummary, ingressSummary, forwardsSummary } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`)
const snapshot = (data, isError = false) => ({ data, isError })
const unavailable = { text: 'Status unavailable', attention: true }
const attention = { text: 'Needs attention', attention: true }
const noRemote = snapshot({ wireguard: { enabled: false }, shadowsocks: { enabled: false } })
const noSocks = snapshot({ enabled: false })

test('loading and read failures never imply that a feature is off', () => {
  assert.deepEqual(socksOutSummary(snapshot(), snapshot()), { text: 'Loading…' })
  assert.deepEqual(socksOutSummary(snapshot(undefined, true), snapshot()), unavailable)
  assert.deepEqual(socksOutSummary(snapshot({ enabled: false }, true), snapshot()), unavailable)
  assert.deepEqual(socksOutSummary(snapshot({ enabled: false }), snapshot(undefined, true)), { text: 'Off' })
  assert.deepEqual(socksOutSummary(snapshot({ enabled: true }), snapshot({ service: { active: true } }, true)), unavailable)
})

test('outbound proxy distinguishes saved intent, stopped service and running TUN', () => {
  const config = snapshot({ enabled: true })
  assert.deepEqual(socksOutSummary(config, snapshot({ enabled: true, service: { active: false } })), attention)
  assert.deepEqual(socksOutSummary(config, snapshot({ enabled: true, service: { active: true }, problem: 'missing backend' })), attention)
  assert.deepEqual(socksOutSummary(config, snapshot({ enabled: true, service: { active: true } })), { text: 'TUN running' })
})

test('remote access includes SOCKS5 even when WireGuard and Shadowsocks are off', () => {
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(), noSocks), { text: 'Off' })
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(), snapshot({ enabled: true, service: { active: true } })), { text: 'SOCKS5 on' })
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(), snapshot(undefined, true)), unavailable)
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(), snapshot({ enabled: true, service_error: 'no D-Bus' })), unavailable)
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(), snapshot({ enabled: true, service: { active: true }, warning: 'unencrypted internet listener' })), attention)
})

test('remote access only reports connections when the tunnel can be observed', () => {
  const config = snapshot({ wireguard: { enabled: true }, shadowsocks: { enabled: true } })
  const wg = { enabled: true, kernel_known: true, interface_present: true, interface_up: true, peers: [{ online: true }, { online: false }] }
  const ss = snapshot({ enabled: true, service: { active: true } })
  assert.equal(remoteSummary(config, snapshot(wg), ss, noSocks).text, '2 devices · 1 connected · Shadowsocks on')
  assert.deepEqual(remoteSummary(config, snapshot({ ...wg, kernel_known: false }), ss, noSocks), unavailable)
  assert.deepEqual(remoteSummary(config, snapshot({ ...wg, interface_up: false }), ss, noSocks), attention)
  assert.deepEqual(remoteSummary(config, snapshot(wg), snapshot({ enabled: true, service: { active: false } }), noSocks), attention)
})

test('ways out excludes blocking destinations and reports failed probes or unknown kernel state', () => {
  const config = snapshot({ enabled: true, exits: [{ via: { kind: 'next_hop' } }, { via: { kind: 'interface' } }, { via: { kind: 'blocked' } }] })
  const status = { enabled: true, known: true, exits: [] }
  assert.equal(gatewaySummary(config, snapshot(status)).text, '2 ways out')
  assert.deepEqual(gatewaySummary(config, snapshot({ ...status, known: false })), unavailable)
  assert.deepEqual(gatewaySummary(config, snapshot({ ...status, exits: [{ probed: true, up: false }] })), attention)
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

test('port forwards do not claim saved rules are in force when kernel state is unknown', () => {
  const config = snapshot({ enabled: true, forwards: [{ name: 'ssh' }] })
  const status = { enabled: true, known: true }
  assert.equal(forwardsSummary(config, snapshot(status)).text, '1 rule')
  assert.deepEqual(forwardsSummary(config, snapshot({ ...status, known: false })), unavailable)
  assert.deepEqual(forwardsSummary(config, snapshot({ ...status, drifted: true })), attention)
})
