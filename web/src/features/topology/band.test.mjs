import assert from 'node:assert/strict'
import { test } from 'node:test'

import { withBand } from './band.ts'

/**
 * The band is one pass over a laid-out map, so these tests hand it a map
 * instead of building one: no device tree is involved in placing it, and the
 * only thing under test is what it adds above the router and how far it moves
 * what is below.
 */
const below = (over = {}) => ({
  width: 1200,
  height: 240,
  narrow: false,
  fits: true,
  router: { x: 480, y: 0, w: 240, h: 64 },
  items: [
    { kind: 'group', key: 'g:home', group: { key: 'g:home', title: 'Home', devices: [], groups: [] }, box: { x: 480, y: 128, w: 240, h: 100 }, variant: 'container', open: false },
    { kind: 'device', key: 'd:aa', device: { mac: 'aa' }, box: { x: 480, y: 168, w: 240, h: 56 } },
  ],
  links: [{ key: 'l:g:home', x0: 600, y0: 64, x1: 600, y1: 128, width: 3 }],
  ...over,
})

const input = (over = {}) => ({
  width: 1200,
  density: 'detail',
  router: { w: 240, h: 64 },
  expanded: new Set(),
  counting: false,
  rated: false,
  ...over,
})

const cards = (out) => out.items.filter((i) => i.kind === 'dialin')

test('a dialed-in device sits above the router and joins it directly', () => {
  const out = withBand(below(), input({ dialIn: { peers: [{ key: 'phone' }] } }))
  const [card] = cards(out)
  assert.equal(card.kind, 'dialin')
  assert.equal(card.key, 'p:phone')
  assert.ok(card.box.y + card.box.h <= out.router.y, 'above the router')
  const line = out.links.find((l) => l.key === 'l:p:phone')
  assert.equal(line.y0, card.box.y + card.box.h, 'leaves the bottom of the card')
  assert.equal(line.y1, out.router.y, 'enters the top of the router')
  assert.equal(line.x1, out.router.x + out.router.w / 2, 'at the router')
})

test('no dialed-in device leaves the map exactly as it was', () => {
  const start = below()
  assert.equal(withBand(start, input()), start)
})

test('each peer is its own card, and no two overlap', () => {
  const out = withBand(below(), input({ dialIn: { peers: [{ key: 'laptop' }, { key: 'phone' }, { key: 'tablet' }] } }))
  const peers = cards(out)
  assert.deepEqual(peers.map((c) => c.key), ['p:laptop', 'p:phone', 'p:tablet'])
  for (const a of peers) {
    for (const b of peers) {
      if (a === b) continue
      const apart = a.box.x + a.box.w <= b.box.x || b.box.x + b.box.w <= a.box.x ||
        a.box.y + a.box.h <= b.box.y || b.box.y + b.box.h <= a.box.y
      assert.ok(apart, `${a.key} and ${b.key} do not overlap`)
    }
  }
})

test('everything above the router is a dialed-in device, one card each', () => {
  // The default routes the kernel reports used to be cards up here as well.
  // Nothing but a peer is, now: the map starts at the router for its own side.
  const out = withBand(below(), input({ dialIn: { peers: [{ key: 'phone' }, { key: 'laptop' }] } }))
  const above = out.items.filter((i) => i.box.y + i.box.h <= out.router.y)
  assert.deepEqual(above.map((i) => i.kind), ['dialin', 'dialin'])
})

test('everything below drops by the height of the band, links included', () => {
  const start = below()
  const out = withBand(start, input({ dialIn: { peers: [{ key: 'phone' }] } }))
  const drop = out.router.y - start.router.y
  assert.ok(drop > 0)
  assert.equal(out.height, start.height + drop, 'the map is taller by the same amount')
  out.items
    .filter((i) => i.kind === 'group' || i.kind === 'device')
    .forEach((item, i) => assert.equal(item.box.y, start.items[i].box.y + drop))
  assert.equal(out.links.find((l) => l.key === 'l:g:home').y0, 64 + drop)
  assert.equal(out.links.find((l) => l.key === 'l:g:home').y1, 128 + drop)
})

test('a phone layout stacks the cards at the left edge, still above the router', () => {
  const out = withBand(below({ width: 375, narrow: true }), input({
    width: 375,
    dialIn: { peers: [{ key: 'phone' }, { key: 'laptop' }] },
  }))
  for (const card of cards(out)) {
    assert.equal(card.box.x, 0)
    assert.ok(card.box.y + card.box.h <= out.router.y)
  }
})

test('more peers than fit in a row wrap, and every one stays above the router', () => {
  const peers = ['a', 'b', 'c', 'd', 'e', 'f'].map((key) => ({ key }))
  const one = withBand(below(), input({ dialIn: { peers: peers.slice(0, 1) } }))
  const out = withBand(below(), input({ dialIn: { peers } }))
  assert.equal(cards(out).length, 6)
  assert.ok(out.router.y > one.router.y, 'the router moves down to clear a second row')
  for (const card of cards(out)) {
    assert.ok(card.box.y + card.box.h <= out.router.y, `${card.key} stays above the router`)
  }
  assert.equal(new Set(cards(out).map((c) => c.box.y)).size, 2, 'six cards at four to a row')
})
