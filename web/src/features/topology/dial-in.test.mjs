import assert from 'node:assert/strict'
import { test } from 'node:test'

import { buildDialIn, dialInState } from './dial-in.ts'

const peer = (over = {}) => ({
  name: 'phone',
  address: '10.6.0.2',
  routes: 'home',
  online: false,
  ...over,
})

test('a tunnel that is off puts nothing on the map', () => {
  assert.equal(buildDialIn(undefined), undefined)
  assert.equal(buildDialIn({ enabled: false, interface: 'wg0', peers: [peer()] }), undefined)
})

test('a tunnel with no devices puts nothing on the map', () => {
  assert.equal(buildDialIn({ enabled: true, interface: 'wg0', peers: [] }), undefined)
})

test('every configured device appears, connected or not', () => {
  const dialIn = buildDialIn({
    enabled: true,
    interface: 'wg0',
    peers: [peer({ name: 'phone', online: true }), peer({ name: 'laptop' })],
  })
  assert.deepEqual(dialIn.peers.map((p) => [p.name, p.state]), [
    ['laptop', 'never'],
    ['phone', 'connected'],
  ])
  assert.equal(dialIn.interface, 'wg0')
})

test('the four states are not collapsed into two', () => {
  assert.equal(dialInState(peer({ online: true })), 'connected')
  assert.equal(dialInState(peer({ last_handshake: '2024-05-01T10:00:00Z' })), 'away')
  assert.equal(dialInState(peer()), 'never')
  assert.equal(dialInState(peer({ unknown: true })), 'unknown')
})

test('names hold the order, so a waking phone does not move a card', () => {
  const names = (peers) => buildDialIn({ enabled: true, interface: 'wg0', peers }).peers.map((p) => p.name)
  const first = names([peer({ name: 'zoe', online: true }), peer({ name: 'adam' })])
  const second = names([peer({ name: 'zoe' }), peer({ name: 'adam', online: true })])
  assert.deepEqual(first, ['adam', 'zoe'])
  assert.deepEqual(first, second)
})

test('absent transfer figures are zero rather than undefined', () => {
  const [only] = buildDialIn({ enabled: true, interface: 'wg0', peers: [peer()] }).peers
  assert.equal(only.rxBytes, 0)
  assert.equal(only.txBytes, 0)
})

test('the card says the label, and keeps the name it is identified by', () => {
  const [only] = buildDialIn({
    enabled: true,
    interface: 'wg0',
    peers: [peer({ name: 'li-mbp', display_name: "Li's Macbook Pro" })],
  }).peers
  assert.equal(only.name, "Li's Macbook Pro")
  assert.equal(only.key, 'li-mbp')
})
