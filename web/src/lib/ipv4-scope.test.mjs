import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ipv4Scope } from './ipv4-scope.ts'

test('private and shared IPv4 ranges have precise boundaries', () => {
  for (const ip of ['192.168.1.2/24', '10.0.0.1', '172.16.0.1', '172.31.255.255']) assert.equal(ipv4Scope(ip), 'private')
  for (const ip of ['100.64.0.0', '100.127.255.255']) assert.equal(ipv4Scope(ip), 'cgnat')
  for (const ip of ['192.0.2.1', '192.169.1.2', '172.15.0.1', '172.32.0.1', '100.63.255.255', '100.128.0.0', '192.168.999.1', 'bad']) assert.equal(ipv4Scope(ip), 'other')
})
