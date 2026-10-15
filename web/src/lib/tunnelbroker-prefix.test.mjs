import assert from 'node:assert/strict'
import { test } from 'node:test'
import { containsSubnet } from '../features/dial/tunnelbroker-prefix.ts'

test('LAN /64 must belong to the saved routed allocation', () => {
  assert.equal(containsSubnet('2001:470:b:652::/64', '2001:0470:000b:0652:0:0:0:0/64'), true)
  assert.equal(containsSubnet('2001:470:abcd::/48', '2001:470:abcd:ffff::/64'), true)
  for (const subnet of ['2001:470:a:652::/64', '2001:470:b:653::/64', '2001:470:b:652::1/64', '2001:470:b:652::/48', 'bad', '192.168.1.0/24', '2001:470:b:652::/64/extra']) {
    assert.equal(containsSubnet('2001:470:b:652::/64', subnet), false, subnet)
  }
  assert.equal(containsSubnet(undefined, '2001:470:b:652::/64'), false)
})
