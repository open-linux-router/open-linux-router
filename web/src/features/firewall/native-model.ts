import { parse, stringify, isSafeNumber, LosslessNumber } from 'lossless-json'

// nft uses uint64 values; browser numbers must not round native expressions.
export function parseNativeJSON(text: string): unknown {
  return parse(text, undefined, (value) =>
    isSafeNumber(value) ? Number(value) : new LosslessNumber(value),
  )
}

export function nativeJSON(value: unknown, space?: number): string {
  const text = stringify(value, undefined, space)
  if (text === undefined) throw new Error('Expected a native JSON value.')
  return text
}

// These are nft's JSON objects, not a second policy language.
export type NftObject = Record<string, unknown>
export interface NftDocument {
  nftables: Record<string, NftObject>[]
}
export interface NativeView {
  ruleset: NftDocument
  saved?: NftDocument
  native: boolean
  revision: string
}

export function ownedRuleset(document: NftDocument): NftDocument {
  return {
    nftables: document.nftables.filter((entry) =>
      Object.entries(entry).some(
        ([kind, obj]) =>
          obj.family === 'inet' &&
          (kind === 'table'
            ? obj.name === 'olr_filter'
            : obj.table === 'olr_filter'),
      ),
    ),
  }
}

export function ruleKey(rule: NftObject, index: number) {
  return `${rule.family}/${rule.table}/${rule.chain}/${rule.handle ?? index}`
}

// Conservative projection: unsupported statements or expressions are never
// silently omitted. Counters affect neither matching nor the verdict.
export function ruleFlow(rule: NftObject) {
  if (!Array.isArray(rule.expr)) return null
  let verdict = ''
  const conditions: string[] = []
  for (const expression of rule.expr as NftObject[]) {
    const keys = Object.keys(expression)
    if (keys.length !== 1) return null
    const key = keys[0]
    if (['accept', 'drop', 'reject'].includes(key)) {
      if (verdict) return null
      verdict = key
    } else if (key === 'counter') {
      continue
    } else if (key === 'match' && !verdict) {
      const match = expression.match as NftObject
      const left = match.left as NftObject | undefined
      if (!left || !['==', '!=', 'in'].includes(String(match.op))) return null
      let field = ''
      const meta = left.meta as NftObject | undefined
      const payload = left.payload as NftObject | undefined
      const ct = left.ct as NftObject | undefined
      if (
        meta &&
        ['iifname', 'oifname', 'l4proto', 'nfproto'].includes(String(meta.key))
      )
        field = String(meta.key)
      else if (
        payload &&
        ['ip', 'ip6', 'tcp', 'udp'].includes(String(payload.protocol)) &&
        ['saddr', 'daddr', 'sport', 'dport'].includes(String(payload.field))
      )
        field = `${payload.protocol} ${payload.field}`
      else if (ct && ['state', 'status'].includes(String(ct.key)))
        field = `ct ${ct.key}`
      else return null
      conditions.push(`${field} ${match.op} ${nativeJSON(match.right)}`)
    } else return null
  }
  if (!verdict) return null
  return {
    conditions: conditions.join(' · ') || 'all packets reaching this rule',
    verdict,
  }
}

export function parseDocument(text: string): NftDocument {
  const doc = parseNativeJSON(text) as NftDocument
  if (!doc || !Array.isArray(doc.nftables))
    throw new Error(
      'Expected a native nft JSON document with a nftables array.',
    )
  return doc
}
