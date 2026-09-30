// Renders the PNG app icons from the one vector logo, public/favicon.svg.
//
// The SVG is the source of truth and is what browsers use for the tab icon, so
// it is sharp at any density. PNGs exist only for the places that will not take
// an SVG: the PWA manifest's install icons, and apple-touch-icon for iOS and
// Safari's "Add to Dock". They are rendered from the vector rather than scaled
// from each other, and the largest is 1024px, so a Dock icon on a 5K display is
// drawn from real pixels rather than upsampled.
//
// Three framings, because the platforms disagree about who draws the tile:
//
//   - **any** — the mark alone on transparency, as drawn in the SVG. Used where
//     the platform shows the icon as it is.
//   - **maskable** — a full-bleed white square with the mark inside the central
//     safe zone, for Android and anything else that crops to its own shape. A
//     transparent icon there gets a black or tinted fill the platform picks.
//   - **apple-touch** — the same, opaque. iOS ignores alpha and fills it with
//     black, then rounds the corners itself.
//
// Chromium does the rasterising, for the reasons given in optimize-icons.mjs:
// it is already present for screenshots and needs no new dependency.
//
// Usage:
//   node scripts/app-icons.mjs
//
// Set CHROME to override browser discovery. Re-run after changing favicon.svg,
// and commit the PNGs it writes to public/.

import { execFileSync } from 'node:child_process'
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const publicDir = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'public')
const svg = readFileSync(path.join(publicDir, 'favicon.svg'), 'utf8')

// The tile colour behind the mark where a platform needs an opaque icon.
const TILE = '#ffffff'

// scale is the mark's size as a fraction of the canvas. The SVG's own artwork
// spans 84% of its viewBox, so 1 keeps that margin. The maskable safe zone is
// a circle of 80% diameter; 0.72 of the canvas puts the ring well inside it.
const icons = [
  { file: 'icon-192.png', size: 192, scale: 1 },
  { file: 'icon-512.png', size: 512, scale: 1 },
  { file: 'icon-1024.png', size: 1024, scale: 1 },
  { file: 'icon-maskable-512.png', size: 512, scale: 0.72, tile: TILE },
  { file: 'icon-maskable-1024.png', size: 1024, scale: 0.72, tile: TILE },
  { file: 'apple-touch-icon.png', size: 180, scale: 0.76, tile: TILE },
  { file: 'apple-touch-icon-1024.png', size: 1024, scale: 0.76, tile: TILE },
]

// Same discovery as optimize-icons.mjs.
function findChrome() {
  if (process.env.CHROME) return process.env.CHROME
  const candidates = [
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
  ]
  for (const root of [
    path.join(process.env.HOME ?? '', '.cache/ms-playwright'),
    path.join(process.env.HOME ?? '', 'Library/Caches/ms-playwright'),
  ]) {
    if (!existsSync(root)) continue
    for (const entry of readdirSync(root)) {
      if (!entry.startsWith('chromium-')) continue
      for (const rel of [
        'chrome-linux64/chrome',
        'chrome-linux/chrome',
        'chrome-mac/Chromium.app/Contents/MacOS/Chromium',
        'chrome-mac-arm64/Chromium.app/Contents/MacOS/Chromium',
      ]) {
        candidates.push(path.join(root, entry, rel))
      }
    }
  }
  const found = candidates.find((c) => existsSync(c))
  if (!found) {
    console.error('no chromium found. Set CHROME=/path/to/chrome, or install one.')
    process.exit(1)
  }
  return found
}

// The page draws the SVG into a canvas of the target size — as a vector, so
// every size is rasterised fresh — and leaves the PNG in the DOM as base64 for
// --dump-dom to hand back.
function page({ size, scale, tile }) {
  const src = 'data:image/svg+xml;base64,' + Buffer.from(svg).toString('base64')
  return `<!doctype html>
<meta charset="utf-8">
<body style="margin:0">
<pre id="out"></pre>
<script>
  const done = (text) => { document.getElementById('out').textContent = text }
  const img = new Image()
  img.onload = () => {
    try {
      const c = document.createElement('canvas')
      c.width = c.height = ${size}
      const g = c.getContext('2d')
      ${tile ? `g.fillStyle = ${JSON.stringify(tile)}; g.fillRect(0, 0, ${size}, ${size})` : ''}
      const d = ${size} * ${scale}
      g.drawImage(img, (${size} - d) / 2, (${size} - d) / 2, d, d)
      done('OK:' + c.toDataURL('image/png').split(',')[1])
    } catch (e) {
      done('ERR:' + e.message)
    }
  }
  img.onerror = () => done('ERR:the SVG did not decode')
  img.src = ${JSON.stringify(src)}
</script>`
}

const chrome = findChrome()
const work = mkdtempSync(path.join(os.tmpdir(), 'olr-app-icons-'))
let failed = 0

for (const icon of icons) {
  const html = path.join(work, `${icon.file}.html`)
  writeFileSync(html, page(icon))

  let dom
  try {
    dom = execFileSync(
      chrome,
      ['--headless', '--disable-gpu', '--no-sandbox', '--virtual-time-budget=15000', '--dump-dom', `file://${html}`],
      { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] },
    )
  } catch (e) {
    console.error(`${icon.file}: chromium failed: ${e.message}`)
    failed++
    continue
  }

  // Only the output element: the <script> source echoed back by --dump-dom
  // contains the literal "OK:" too.
  const out = dom.match(/<pre id="out">([\s\S]*?)<\/pre>/)
  const payload = out ? out[1].trim() : ''
  const match = payload.match(/^OK:([A-Za-z0-9+/=]+)$/)
  if (!match) {
    console.error(`${icon.file}: ${payload.startsWith('ERR:') ? payload.slice(4) : 'no image data came back'}`)
    failed++
    continue
  }

  const buf = Buffer.from(match[1], 'base64')
  writeFileSync(path.join(publicDir, icon.file), buf)
  console.log(`${icon.file.padEnd(28)} ${String(icon.size).padStart(4)}px  ${String(Math.round(buf.length / 1024)).padStart(3)} KiB`)
}

rmSync(work, { recursive: true, force: true })
process.exit(failed ? 1 : 0)
