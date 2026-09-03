// Regenerates src/lib/config-types.ts from olrd's published schema.
//
// This is design.md §3.2 rule 3 made concrete: a module's tagged Go struct is
// the single source for the CLI flags, the REST body, the UI form, and the MCP
// tool definition. Add a field in Go, run this, and TypeScript knows about it.
// Nothing in the UI hand-writes a config shape.
//
// Only *config* is covered, because only config is reflected. The observed
// read surface (plan, status, leases) is hand-typed in src/lib/api-types.ts —
// see the note there.
//
// Usage: node scripts/gen-types.mjs [baseUrl]
//   olrd must be running. Default http://127.0.0.1:8080.

import { writeFile } from 'node:fs/promises'
import path from 'node:path'

import { compile } from 'json-schema-to-typescript'

const base = process.argv[2] ?? 'http://127.0.0.1:8080'
const out = path.resolve(import.meta.dirname, '../src/lib/config-types.ts')

const header = `// Code generated from olrd's published JSON Schema. DO NOT EDIT.
//
// Regenerate with \`make types\` (olrd must be running).
// Source of truth: the Go config structs — see design.md §3.2 rule 3.
`

let response
try {
  response = await fetch(`${base}/api/schema`)
} catch (cause) {
  console.error(
    `could not reach olrd at ${base}. Start it first, e.g.\n` +
      `  olrd --listen 127.0.0.1:8080 --no-auth --root /tmp/olr-dev\n`,
  )
  throw cause
}
if (!response.ok) {
  throw new Error(`GET ${base}/api/schema returned ${response.status}`)
}

const { modules } = await response.json()

// Every module compiled in **one** pass, as properties of the whole document.
//
// It used to be one compile() per module, which was fine for two of them and
// broke on the third: the generator dedupes type names within a compilation,
// so `Duration` and `IPAddress` were unique inside dhcp and inside routing but
// collided once both were in the file. Compiling together lets it do the
// renaming it already knows how to do, and it costs nothing — the wrapper
// interface it produces is the shape of olr.json itself, which is a type worth
// having anyway.
//
// The full projection, not the relaxed one: these types describe a complete
// document, which is what the UI holds in state and sends with PUT. The relaxed
// projection exists for PATCH validation on the server, where optionality is
// the point; expressing it here would make every field optional and delete the
// type safety this file exists for.
const names = Object.keys(modules).sort()
const properties = {}
const defs = {}

for (const name of names) {
  // Each module publishes its own `$defs` at *its* root, and every `$ref`
  // inside it is written as `#/$defs/X`. Nested under a wrapper those pointers
  // no longer resolve, so the definitions are hoisted to the document root and
  // the references rewritten to match.
  //
  // A key is only renamed when it is already taken by a different module —
  // `dhcp` and `routing` both define a `Duration`, and they are different types
  // with different formats. Renaming unconditionally would have been simpler
  // and wrong: the generator names a definition after its key when it has no
  // title, so a blanket prefix turns `Pool` into `Dhcp_Pool` across the whole
  // UI for the sake of one collision.
  const { $defs, ...schema } = structuredClone(modules[name].full)

  /** The renames this module needs, `X` → `X_routing`. */
  const renamed = {}
  for (const [key, value] of Object.entries($defs ?? {})) {
    const unique = key in defs ? `${key}_${name}` : key
    if (unique !== key) renamed[key] = unique
    defs[unique] = value
  }

  for (const value of Object.values($defs ?? {})) rewriteRefs(value, renamed)
  rewriteRefs(schema, renamed)
  properties[name] = schema
}

/** Applies a `$defs` rename map to every `$ref` in a subtree, in place. */
function rewriteRefs(node, renamed) {
  if (Array.isArray(node)) {
    for (const item of node) rewriteRefs(item, renamed)
    return
  }
  if (!node || typeof node !== 'object') return

  if (typeof node.$ref === 'string' && node.$ref.startsWith('#/$defs/')) {
    const key = node.$ref.slice('#/$defs/'.length)
    if (key in renamed) node.$ref = `#/$defs/${renamed[key]}`
  }
  for (const value of Object.values(node)) rewriteRefs(value, renamed)
}

const document = {
  $schema: 'https://json-schema.org/draft/2020-12/schema',
  title: 'OlrDocument',
  description:
    "The whole box's configuration, one property per module — the shape of " +
    '/etc/open-linux-router/olr.json.',
  type: 'object',
  additionalProperties: false,
  properties,
  $defs: defs,
}

const ts = await compile(document, 'OlrDocument', {
  bannerComment: '',
  additionalProperties: false,
  style: { semi: false, singleQuote: true },
})

await writeFile(out, header + '\n' + ts, 'utf8')
console.log(`wrote ${path.relative(process.cwd(), out)}`)
