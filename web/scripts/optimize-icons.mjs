// Downscales and re-encodes device icon masters for the SPA.
//
// The masters are ~1 MB, 1024×1024 PNGs; the UI never shows one larger than a
// row thumbnail or a detail panel. These assets are embedded into the olrd
// binary (internal/webui), so their size is the binary's size, on a device that
// may have very little flash. That is why this step exists at all.
//
// Chromium does the work, because it is the one image pipeline we can rely on
// having without adding a dependency. `sharp` would be a native module in a
// project with three direct Go dependencies and a deliberate stdlib-first
// stance; ImageMagick would be a system package the contributor has to install.
// A headless browser is already present for screenshots, decodes PNG properly,
// resamples well, and encodes WebP — and it is driven here through plain CLI
// flags, with no puppeteer or playwright package required.
//
// Usage:
//   node scripts/optimize-icons.mjs <master-dir> [--out <dir>] [--size 256]
//                                   [--quality 0.86] [--format webp|png]
//
// Set CHROME to override browser discovery.

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync, rmSync } from 'node:fs'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

const args = process.argv.slice(2)
if (args.length === 0 || args[0].startsWith('--')) {
  console.error(
    'usage: node scripts/optimize-icons.mjs <master-dir> [--out dir] ' +
      '[--size 256] [--quality 0.86] [--format webp|png]',
  )
  process.exit(2)
}

const masterDir = path.resolve(args[0])
const opt = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i === -1 ? fallback : args[i + 1]
}

const outDir = path.resolve(
  opt('out', path.resolve(import.meta.dirname, '../src/assets/device-icons')),
)
const size = Number(opt('size', 256))
const quality = Number(opt('quality', 0.86))
const format = opt('format', 'webp')

if (!['webp', 'png'].includes(format)) {
  console.error(`unsupported --format ${format}`)
  process.exit(2)
}

// Candidate browsers, in order. The playwright cache is checked because it is
// what this repo already downloads for screenshots.
function findChrome() {
  if (process.env.CHROME) return process.env.CHROME

  const candidates = [
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
  ]

  const cacheRoots = [
    path.join(process.env.HOME ?? '', '.cache/ms-playwright'),
    path.join(process.env.HOME ?? '', 'Library/Caches/ms-playwright'),
  ]
  for (const root of cacheRoots) {
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
    console.error(
      'no chromium found. Set CHROME=/path/to/chrome, or install one.\n' +
        'Searched:\n  ' + candidates.join('\n  '),
    )
    process.exit(1)
  }
  return found
}

const chrome = findChrome()

// The page draws the master into a canvas of the target size and leaves the
// encoded result in the DOM as base64, which --dump-dom hands back on stdout.
//
// Fitted rather than stretched, and centred, so that a master which is not
// square keeps its proportions and its optical weight — the style spec requires
// consistent weight across categories, and a squashed printer would break it.
function page(srcUrl, size, format, quality) {
  return `<!doctype html>
<meta charset="utf-8">
<body style="margin:0">
<pre id="out"></pre>
<script>
  const done = (text) => { document.getElementById('out').textContent = text }
  const img = new Image()
  // Synchronous on purpose. --dump-dom serialises at the load event, and an
  // async continuation — even one awaiting img.decode() — lands after that, so
  // the page would hand back an empty element. onload already guarantees the
  // bitmap is available to drawImage.
  img.onload = () => {
    try {
      const c = document.createElement('canvas')
      c.width = ${size}; c.height = ${size}
      const ctx = c.getContext('2d')
      ctx.imageSmoothingEnabled = true
      ctx.imageSmoothingQuality = 'high'
      const scale = Math.min(${size} / img.naturalWidth, ${size} / img.naturalHeight)
      const w = img.naturalWidth * scale, h = img.naturalHeight * scale
      ctx.drawImage(img, (${size} - w) / 2, (${size} - h) / 2, w, h)
      const url = c.toDataURL('image/${format}', ${quality})
      const comma = url.indexOf(',')
      const mime = url.slice(5, url.indexOf(';'))
      done('OK:' + mime + ':' + url.slice(comma + 1))
    } catch (e) {
      done('ERR:' + e.message)
    }
  }
  img.onerror = () => done('ERR:could not load the master image')
  img.src = ${JSON.stringify(srcUrl)}
</script>
</body>`
}

const masters = readdirSync(masterDir)
  .filter((f) => f.toLowerCase().endsWith('.png'))
  .sort()

if (masters.length === 0) {
  console.error(`no .png masters in ${masterDir}`)
  process.exit(1)
}

mkdirSync(outDir, { recursive: true })
const work = mkdtempSync(path.join(tmpdir(), 'olr-icons-'))

let totalIn = 0
let totalOut = 0
let failed = 0

for (const master of masters) {
  const src = path.join(masterDir, master)
  const name = path.basename(master, '.png')
  const html = path.join(work, `${name}.html`)

  writeFileSync(html, page(`file://${src}`, size, format, quality))

  let dom
  try {
    dom = execFileSync(
      chrome,
      [
        '--headless',
        '--disable-gpu',
        '--no-sandbox',
        // Reading a file:// master from a file:// page is a cross-origin read
        // as far as the canvas is concerned, and a tainted canvas cannot be
        // read back. This is a local build step over files we just wrote.
        '--allow-file-access-from-files',
        // Advances the clock so the async decode and encode finish before the
        // DOM is dumped, rather than relying on them beating the load event.
        '--virtual-time-budget=15000',
        '--dump-dom',
        `file://${html}`,
      ],
      { encoding: 'utf8', maxBuffer: 256 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] },
    )
  } catch (e) {
    console.error(`${name}: chromium failed: ${e.message}`)
    failed++
    continue
  }

  // Read only the output element. Matching the whole dump would also match the
  // literal "OK:"/"ERR:" strings inside the <script> source that --dump-dom
  // echoes back, which reports a phantom failure for every icon.
  const out = dom.match(/<pre id="out">([\s\S]*?)<\/pre>/)
  const payload = out ? out[1].trim() : ''

  const match = payload.match(/^OK:([\w/+.-]+):([A-Za-z0-9+/=]+)$/)
  if (!match) {
    const err = payload.match(/^ERR:([\s\S]*)$/)
    console.error(
      `${name}: ${err ? err[1] : `no image data came back from chromium (got ${payload.length} chars)`}`,
    )
    failed++
    continue
  }

  const [, mime, b64] = match
  const wanted = `image/${format}`
  if (mime !== wanted) {
    // toDataURL silently falls back to PNG for a format the browser cannot
    // encode. Saying so beats writing a PNG under a .webp name, which would
    // be served with the wrong content type forever after.
    console.error(
      `${name}: chromium encoded ${mime}, not ${wanted}; ` +
        `re-run with --format ${mime.replace('image/', '')}`,
    )
    failed++
    continue
  }

  const buf = Buffer.from(b64, 'base64')
  const dest = path.join(outDir, `${name}.${format}`)
  writeFileSync(dest, buf)

  const inSize = readFileSync(src).length
  totalIn += inSize
  totalOut += buf.length
  console.log(
    `${name.padEnd(12)} ${String(Math.round(inSize / 1024)).padStart(5)} KiB -> ` +
      `${String(Math.round(buf.length / 1024)).padStart(4)} KiB  ${size}×${size} ${format}`,
  )
}

rmSync(work, { recursive: true, force: true })

console.log(
  `\n${masters.length - failed}/${masters.length} icons: ` +
    `${Math.round(totalIn / 1024)} KiB -> ${Math.round(totalOut / 1024)} KiB  (${outDir})`,
)
if (failed > 0) process.exit(1)
