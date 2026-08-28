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

const chunks = [header]
for (const [name, projections] of Object.entries(modules).sort()) {
  // The full projection, not the relaxed one: these types describe a complete
  // document, which is what the UI holds in state and sends with PUT. The
  // relaxed projection exists for PATCH validation on the server, where
  // optionality is the point; expressing it here would make every field
  // optional and delete the type safety this file exists for.
  const ts = await compile(projections.full, name, {
    bannerComment: '',
    additionalProperties: false,
    style: { semi: false, singleQuote: true },
  })
  chunks.push(ts)
}

await writeFile(out, chunks.join('\n'), 'utf8')
console.log(`wrote ${path.relative(process.cwd(), out)}`)
