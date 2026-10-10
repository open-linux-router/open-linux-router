import assert from 'node:assert/strict'
import { test } from 'node:test'

import { groupOpenings, trafficBranches } from './traffic-model.ts'

const status = {
  enabled: true, known: true, drifted: false, inside: ['ens19', 'wg0'],
  openings: [], blocked_input: 521, blocked_forward: 38,
  plan: { changes: [], empty: true, impact: 'none' },
}

test('only observed, verified drops are reported as counted packets', () => {
  const blocked = trafficBranches(status)[2]
  assert.equal(blocked.detail, '559 packets')
  assert.equal(blocked.tone, 'attention')
  assert.match(blocked.explanation, /521 packets to this router and 38 to inside devices/)
  assert.match(blocked.explanation, /does not necessarily mean malicious/)
  assert.equal(trafficBranches({ ...status, blocked_input: 0, blocked_forward: 0 })[2].detail, '0 packets')
})

test('unknown, drifted and unbuildable policies never claim verified counts', () => {
  for (const change of [{ known: false }, { drifted: true }, { problem: 'No inside networks' }]) {
    const branch = trafficBranches({ ...status, ...change })[2]
    assert.equal(branch.detail, 'Count not verified')
    assert.equal(branch.tone, 'inactive')
    assert.doesNotMatch(branch.explanation, /521|38|559/)
  }
})

test('off state makes no protection or counter claim', () => {
  const branches = trafficBranches({ ...status, enabled: false })
  assert.ok(branches.every((branch) => branch.tone === 'inactive'))
  assert.equal(branches[2].title, 'No inbound filtering')
  assert.equal(branches[2].detail, 'Firewall is off')
})

test('grouped openings retain each protocol, port and broker source restriction', () => {
  assert.deepEqual(groupOpenings([
    { for: 'ingress', protocol: 'tcp', port: 443 },
    { for: 'ingress', protocol: 'udp', port: 443 },
    { for: 'IPv6 tunnel', protocol: '6in4', from: '203.0.113.1' },
  ]), [
    { name: 'ingress', matches: ['tcp 443', 'udp 443'] },
    { name: 'IPv6 tunnel', matches: ['6in4 from 203.0.113.1'] },
  ])
  assert.deepEqual(groupOpenings([]), [])
})
