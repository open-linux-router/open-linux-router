import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, ApiError } from './api.ts'

test('a draft carries its original revision through spread and newer reads', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  let revision = '"first"'
  globalThis.fetch = async (path, init) => {
    requests.push({ path, ...init })
    return new Response('{"enabled":true}', { headers: { ETag: revision } })
  }
  try {
    const first = await api.get('/api/dhcp/config')
    const draft = { ...first, enabled: false }
    revision = '"second"'
    await api.get('/api/dhcp/config')
    await api.put('/api/dhcp/config', draft)
    assert.equal(requests.at(-1).headers['If-Match'], '"first"')
    assert.equal(requests.at(-1).body, '{"enabled":false}')
    await api.send('PATCH', '/api/firewall/config', draft, { 'If-Match': 'native' })
    assert.equal(requests.at(-1).headers['If-Match'], 'native')
    globalThis.fetch = async () => new Response('{"error":{"message":"reload"}}', { status: 412 })
    await assert.rejects(api.put('/api/dhcp/config', draft), (error) => error instanceof ApiError && error.status === 412)
  } finally {
    globalThis.fetch = originalFetch
  }
})
