import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nativeJSON, parseNativeJSON, ownedRuleset, parseDocument, ruleFlow, ruleKey } from './native-model.ts'

test('native projection preserves conditions and excludes complex statements', () => {
  const rule = { expr: [{ match: { op: '==', left: { meta: { key: 'iifname' } }, right: 'guest' } }, { counter: { packets: 20 } }, { drop: null }] }
  assert.deepEqual(ruleFlow(rule), { conditions: 'iifname == "guest"', verdict: 'drop' })
  for (const expr of [[{ jump: { target: 'custom' } }], [{ log: null }, { accept: null }], [{ mangle: {} }, { accept: null }], [{ counter: null }], [{ accept: null }, { match: { left: {}, op: '==' } }]]) assert.equal(ruleFlow({ expr }), null)
})

test('native uint64 values survive reading, editing and request encoding', () => {
  const raw = '{"nftables":[{"quota":{"family":"inet","table":"olr_filter","name":"limit","bytes":9007199254740993}}]}'
  const doc = parseDocument(raw)
  assert.equal(nativeJSON(doc), raw)
  const response = parseNativeJSON('{"ruleset":' + raw + ',"native":true}')
  assert.match(nativeJSON(response.ruleset), /9007199254740993/)
  assert.match(nativeJSON({ ruleset: doc }), /9007199254740993/)
})

test('only the owned table is selected for editing, original expressions survive', () => {
  const document = parseDocument(JSON.stringify({ nftables: [{ table: { family: 'inet', name: 'docker' } }, { table: { family: 'inet', name: 'olr_filter' } }, { rule: { family: 'inet', table: 'olr_filter', chain: 'input', handle: 4, expr: [{ jump: { target: 'custom' } }] } }] }))
  assert.equal(ownedRuleset(document).nftables.length, 2)
  assert.deepEqual(ownedRuleset(document).nftables[1].rule.expr, [{ jump: { target: 'custom' } }])
  assert.equal(ruleKey(document.nftables[2].rule, 0), 'inet/olr_filter/input/4')
  assert.throws(() => parseDocument('{}'))
})
