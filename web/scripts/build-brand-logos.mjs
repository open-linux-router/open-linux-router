// Bundles theSVG's brand marks into the two files internal/brandlogo embeds.
//
// Every mark the pickers offer used to be fetched from githubusercontent at
// render time, which made a router page depend on GitHub being reachable and
// painted a pending spinner whenever it was not. The artwork is the same for
// every installation, so it is packed into the binary with the rest of the
// static assets.
//
// It is built here rather than committed because the library is artwork, not
// code: keeping it out of the repository means `npm install` is the whole
// update — bump @thesvg/icons, rebuild — and a 6 MB blob never lands in a diff
// or in git history. The package is pinned exactly, so the marks a build ships
// are the marks the lockfile names.
//
// Two files come out, both gitignored:
//
//   logos.bin   one gzip stream per mark, concatenated in slug order
//   logos.idx   "slug<TAB>offset<TAB>length", one line per mark, sorted by slug
//
// The split is what makes one mark cheap to serve: the daemon slices logos.bin
// and hands the browser a gzip body, so no decompression happens on the router
// at all, and memory stays flat however large the catalogue grows. Searching is
// a scan of the index, which is slugs only.
//
// Runs as `prebuild`, so `npm run build` and `make web` produce it; `make
// brand-logos` regenerates it without touching the SPA.

import { existsSync } from 'node:fs'
import { readdir, readFile, writeFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { gzipSync } from 'node:zlib'

const source = path.resolve(process.argv[2] ?? path.join(import.meta.dirname, '../node_modules/@thesvg/icons'))
const dist = path.join(source, 'dist')
const out = path.resolve(import.meta.dirname, '../../internal/brandlogo/data')

if (!existsSync(dist)) {
  console.error(
    `${dist} is missing. The catalogue is built from the @thesvg/icons dev dependency:\n` +
      `  cd web && npm install\n` +
      `then run this again (\`npm run build\` does both).`,
  )
  process.exit(1)
}

/** The `default` variant is what public/icons/<slug>/default.svg is upstream. */
function extract(source) {
  const match = source.match(/export const svg = `([\s\S]*?)`;?\n/)
  if (!match) return undefined
  let mark = match[1]
  // An XML prolog and a DOCTYPE say nothing an <img> needs, and the DOCTYPE
  // names an external DTD — the one thing in the catalogue that still points
  // off the box. Everything before the root element is dropped except the
  // provenance comments.
  const root = mark.indexOf('<svg')
  if (root < 0) return undefined
  const prolog = mark.slice(0, root).replace(/<\?xml[\s\S]*?\?>/gi, '').replace(/<!DOCTYPE[^>]*>/gi, '')
  return (prolog + mark.slice(root)).trim()
}

const slugs = (await readdir(dist))
  .filter((file) => file.endsWith('.js'))
  .map((file) => file.slice(0, -3))
  .sort()

const chunks = []
const index = []
let offset = 0
let raw = 0

for (const slug of slugs) {
  const svg = extract(await readFile(path.join(dist, `${slug}.js`), 'utf8'))
  if (!svg) {
    console.error(`skipping ${slug}: no default variant`)
    continue
  }
  // Anything the daemon would have to sanitize at runtime is a bug in the
  // catalogue, not a request to filter: refuse to build it in. A leading
  // comment carrying the mark's provenance is fine and is kept.
  if (!/^(<!--[\s\S]*?-->\s*)*<svg[\s>]/.test(svg) || /<script|<foreignObject|on\w+=/i.test(svg)) {
    console.error(`skipping ${slug}: not a plain mark`)
    continue
  }
  const packed = gzipSync(Buffer.from(svg, 'utf8'), { level: 9 })
  chunks.push(packed)
  index.push(`${slug}\t${offset}\t${packed.length}`)
  offset += packed.length
  raw += Buffer.byteLength(svg)
}

if (!index.length) {
  console.error('no marks found; is the path an @thesvg/icons package?')
  process.exit(1)
}

await mkdir(out, { recursive: true })
await writeFile(path.join(out, 'logos.bin'), Buffer.concat(chunks))
await writeFile(path.join(out, 'logos.idx'), `${index.join('\n')}\n`)

const mib = (bytes) => `${(bytes / 1048576).toFixed(2)} MiB`
console.log(`${index.length} marks: ${mib(raw)} of SVG in ${mib(offset)} of gzip`)
