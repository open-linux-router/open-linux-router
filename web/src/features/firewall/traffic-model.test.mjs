import assert from 'node:assert/strict'
import { test } from 'node:test'
import { trafficFlows } from './traffic-model.ts'

const row = (overrides = {}) => ({ id: 'rule', source: 'outside', category: 'ICMPv6', description: 'IPv6 control messages', destination: 'router', verdict: 'allowed', packets: 10, bytes: 800, ...overrides })

test('zero-traffic categories never appear, including zero drops', () => {
  assert.deepEqual(trafficFlows([row({ packets: 0 }), row({ packets: 0, verdict: 'blocked' })]), [])
})

test('input and forward counts merge without crossing source or verdict', () => {
  const data = [row(), row({ destination: 'forwarded', description: 'IPv6 errors', packets: 4, bytes: 300 }), row({ source: 'inside' }), row({ source: 'local' }), row({ verdict: 'blocked' })]
  const flows = trafficFlows(data)
  assert.equal(flows.length, 4)
  assert.equal(flows[0].packets, 14)
  assert.equal(flows[0].bytes, 1100)
  assert.deepEqual(flows[0].destinations, ['router', 'forwarded'])
  assert.deepEqual(flows[0].descriptions, ['IPv6 control messages', 'IPv6 errors'])
  assert.equal(flows.reduce((sum, flow) => sum + flow.packets, 0), 44)
  assert.equal(data[0].packets, 10)
})
