import assert from 'node:assert/strict'
import { test } from 'node:test'

import { servicesByDevice } from './services.ts'

test('published services stay with the same device after rename and name reuse', () => {
  const mac = 'aa:bb:cc:00:00:01'
  const service = { name: 'files', upstream: { device: mac, port: 80 } }
  const devices = [
    { mac, name: 'storage' },
    { mac: 'aa:bb:cc:00:00:02', name: 'nas' },
  ]
  const result = servicesByDevice({ enabled: true, services: [service] }, devices)
  assert.deepEqual(result.get(mac), [service])
  assert.equal(result.has(devices[1].mac), false)
})
