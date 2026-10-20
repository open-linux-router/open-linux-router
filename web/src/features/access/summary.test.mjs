import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('./summary.ts', import.meta.url), 'utf8')
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
const { gatewaySummary, socksOutSummary, remoteSummary, ingressSummary, systemSummary, tunnelBrokerSummary } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`)
const snapshot = (data, isError = false) => ({ data, isError })
const unavailable = { text: 'Status unavailable', attention: true }
const attention = { text: 'Needs attention', attention: true }
const noRemote = snapshot({ wireguard: { enabled: false } })
const noSocks = snapshot({ enabled: false })

test('individual ways in do not inherit unrelated service failures', () => {
  const failed = snapshot(undefined, true)
  assert.deepEqual(remoteSummary(noRemote, failed, failed, 'wireguard'), { text: 'Off' })
  assert.deepEqual(remoteSummary(failed, failed, noSocks, 'socks'), { text: 'Off' })
  assert.deepEqual(remoteSummary(noRemote, failed, failed, 'socks'), unavailable)
})

test('system default distinguishes no route from an unavailable observation', () => {
  assert.deepEqual(systemSummary(snapshot()), { text: 'Loading…' })
  assert.deepEqual(systemSummary(snapshot(undefined, true)), unavailable)
  assert.deepEqual(systemSummary(snapshot({ default_routes: [] })), { text: 'No default route', attention: true })
  assert.equal(systemSummary(snapshot({ default_routes: [{ family: 4, via: '192.0.2.1', dev: 'eth0' }, { family: 6, dev: 'ppp0' }] })).text, 'IPv4 via 192.0.2.1 · eth0 · IPv6 via ppp0')
})

test('HE inventory is distinct from activation and matching runtime state', () => {
  assert.deepEqual(tunnelBrokerSummary(snapshot({}), snapshot()), { text: 'Not configured' })
  assert.deepEqual(tunnelBrokerSummary(snapshot({ tunnelbroker: { enabled: false } }), snapshot(undefined, true)), { text: 'Saved · Off' })
  const active = snapshot({ tunnelbroker: { enabled: true } })
  assert.deepEqual(tunnelBrokerSummary(active, snapshot(undefined, true)), unavailable)
  assert.deepEqual(tunnelBrokerSummary(active, snapshot({ present: true, up: true, matches: false })), attention)
  assert.deepEqual(tunnelBrokerSummary(active, snapshot({ present: true, up: true, matches: true })), { text: 'Tunnel configured' })
})

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

test('remote access includes SOCKS5 even when WireGuard is off', () => {
  assert.deepEqual(remoteSummary(noRemote, snapshot(), noSocks), { text: 'Off' })
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot({ enabled: true, service: { active: true } })), { text: 'SOCKS5 on' })
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot(undefined, true)), unavailable)
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot({ enabled: true, service_error: 'no D-Bus' })), unavailable)
  assert.deepEqual(remoteSummary(noRemote, snapshot(), snapshot({ enabled: true, service: { active: true }, warning: 'unencrypted internet listener' })), attention)
})

test('remote access only reports connections when the tunnel can be observed', () => {
  const config = snapshot({ wireguard: { enabled: true } })
  const wg = { enabled: true, kernel_known: true, interface_present: true, interface_up: true, peers: [{ online: true }, { online: false }] }
  assert.equal(remoteSummary(config, snapshot(wg), noSocks).text, '2 devices · 1 connected')
  assert.deepEqual(remoteSummary(config, snapshot({ ...wg, kernel_known: false }), noSocks), unavailable)
  assert.deepEqual(remoteSummary(config, snapshot({ ...wg, interface_up: false }), noSocks), attention)
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
