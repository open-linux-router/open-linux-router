import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

/**
 * Keeps the control language from drifting back into page-local decisions.
 *
 * `docs/ui-control-language.md` says a state's colour belongs to the word, a row
 * glyph means one verb, and a switch is labelled by something the operator can
 * see. Prose alone did not hold that line for the last ten pages, so the rules
 * that can be checked mechanically are checked here.
 *
 * Run with `node scripts/test-control-language.mjs`. It reads JSX as text on
 * purpose: the point is to catch a class string or a prop that was typed by
 * hand, which a type checker cannot see.
 */
const root = new URL('../src', import.meta.url).pathname
const problems = []

/** Files allowed to name a tone colour, because their hue is not a status. */
const colourOwners = new Set([
  'components/ui/status.tsx',
  'components/ui/badge.tsx',
  'components/ui/alert.tsx',
  'components/ui/button.tsx',
  'components/ui/primary-metric.tsx',
  // Chart and map scales: hue is data, not state.
  'features/network-quality/network-quality.tsx',
  'features/topology/nodes.tsx',
  'features/setup/first-run.tsx',
  // The Overview verdict is a sentence with one icon, not a status glyph.
  'routes/overview.tsx',
])

const files = []
function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) walk(path)
    else if (path.endsWith('.tsx')) files.push(path)
  }
}
walk(root)

const rel = (path) => relative(root, path).split(sep).join('/')

for (const file of files) {
  const name = rel(file)
  const source = readFileSync(file, 'utf8')
  const lines = source.split('\n')

  lines.forEach((line, index) => {
    const at = `${name}:${index + 1}`

    // 1. A status glyph or a tinted status word may be drawn only where the
    //    tone map lives. Warning surfaces (Alert borders and fills) are a
    //    different job and are left to the alert component.
    const statusDot = /\bbg-(success|warning|destructive)(\/\d+)?\b/.test(line) && /rounded-full/.test(line)
    const neutralDot = /\bbg-muted-foreground\/\d+\b/.test(line)
    if (!colourOwners.has(name) && (statusDot || neutralDot)) {
      problems.push(`${at} writes a status colour; name a word from lib/state.ts and use StatusDot/StatusInline/StatusPill`)
    }

    // 2. One dot scale: 6px and 8px, nothing in between.
    if (/\bsize-2\.5\b/.test(line) && /rounded-full/.test(line)) {
      problems.push(`${at} draws a 10px status dot; use StatusDot (6px inline, 8px verdict)`)
    }
  })

  // 3. Every switch names its subject somewhere on screen. JSX props contain
  //    `=>`, so the tag ends at its self-closing `/>` rather than at the first
  //    `>`.
  for (const match of source.matchAll(/<Switch\b/g)) {
    const rest = source.slice(match.index)
    const end = rest.indexOf('/>')
    if (end < 0) continue
    const tag = rest.slice(0, end)
    if (!/\b(aria-label|aria-labelledby|id)=/.test(tag)) {
      const line = source.slice(0, match.index).split('\n').length
      problems.push(`${name}:${line} <Switch> has no aria-label, aria-labelledby or id`)
    }
  }

  // 4. Accepting a plan warning is not a destructive action.
  for (const match of source.matchAll(/<Button\b([\s\S]*?)>([\s\S]*?)<\/Button>/g)) {
    const [, attrs, body] = match
    if (attrs.includes('variant="destructive"') && /anyway/i.test(body)) {
      const line = source.slice(0, match.index).split('\n').length
      problems.push(`${name}:${line} paints "anyway" destructive; it accepts a warning and is the primary action`)
    }
  }

  // 5. Green is not a badge: a status is a dot and a word, not a tinted pill in a row.
  if (!name.startsWith('features/plan/') && !name.startsWith('features/nat/plan-preview')) {
    if (/<Badge[^>]*variant="success"/.test(source)) {
      problems.push(`${name} renders a state as a success Badge; use StatusInline or StatusPill`)
    }
  }
}

if (problems.length > 0) {
  console.error('Control language violations:\n' + problems.map((problem) => `  ${problem}`).join('\n'))
  process.exit(1)
}
console.log(`Control language: ${files.length} files checked, no violations.`)
