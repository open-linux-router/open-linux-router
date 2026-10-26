import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { brandLogoHref, brandLogoSlug, brandLogoSrc, searchBrandLogos } from './brand-logos.ts'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
})

test('a thesvg value is a slug and nothing else is', () => {
  assert.equal(brandLogoSlug('thesvg:grafana'), 'grafana')
  assert.equal(brandLogoSlug('thesvg:home-assistant'), 'home-assistant')
  assert.equal(brandLogoSlug('os/debian'), '')
  assert.equal(brandLogoSlug('apple/laptop'), '')
  assert.equal(brandLogoSlug(''), '')
  assert.equal(brandLogoSlug(undefined), '')
})

test('a mark is a URL on this router, never a third party', () => {
  assert.equal(brandLogoSrc('grafana'), '/brand-logos/grafana')
  assert.equal(brandLogoHref('thesvg:grafana'), '/brand-logos/grafana')
  assert.equal(brandLogoHref('os/debian'), undefined)
  for (const href of [brandLogoSrc('grafana'), brandLogoSrc('tp-link')]) {
    assert.match(href, /^\/brand-logos\//)
  }
})

test('a search asks the daemon once per query and remembers the answer', async () => {
  const asked = []
  globalThis.fetch = async (path) => {
    asked.push(path)
    return new Response(JSON.stringify({ slugs: ['grafana', 'aws-amazon-managed-grafana'] }), {
      headers: { 'Content-Type': 'application/json' },
    })
  }

  assert.deepEqual(await searchBrandLogos('GRAFANA'), ['grafana', 'aws-amazon-managed-grafana'])
  assert.equal(asked.length, 1)
  assert.equal(asked[0], '/brand-logos?q=grafana', 'the query is lowercased and encoded')

  assert.deepEqual(await searchBrandLogos('grafana '), ['grafana', 'aws-amazon-managed-grafana'])
  assert.equal(asked.length, 1, 'the second identical search was served from the cache')
})

test('an empty query asks nothing, and a broken answer is no answer', async () => {
  let asked = 0
  globalThis.fetch = async () => {
    asked += 1
    return new Response('not json', { status: 500 })
  }

  assert.deepEqual(await searchBrandLogos('   '), [])
  assert.equal(asked, 0)

  assert.deepEqual(await searchBrandLogos('unavailable'), [])
  assert.equal(asked, 1)
})
