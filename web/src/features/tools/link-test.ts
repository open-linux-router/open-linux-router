/**
 * The device-to-router test, driven from OLR's own page.
 *
 * LibreSpeed's engine is the measurement (vendored under
 * internal/tools/librespeed/, LGPLv3, unmodified); this module is only the
 * bridge to it. The engine is a classic script that defines the global
 * `Speedtest` and builds its worker from a URL relative to the document, so
 * both the script and the worker are pointed at /link-test/ rather than at the
 * route the tool page happens to live on.
 */

/** Where olrd serves the vendored engine, its worker and the test endpoints. */
export const LINK_TEST_PREFIX = '/link-test'

/** One status message from the worker; only the last one is used. */
export interface LinkTestSample {
  testState: number
  dlStatus: string
  ulStatus: string
  pingStatus: string
  jitterStatus: string
}

/** What the page shows once the test ends. */
export interface LinkTestResult {
  latency: number
  jitter: number | null
  download: number
  upload: number
}

interface LinkTestServer {
  name: string
  server: string
  dlURL: string
  ulURL: string
  pingURL: string
  getIpURL: string
}

/**
 * The engine as the page uses it, narrowed to the calls this bridge makes.
 * Everything else in speedtest.js is the engine's own business.
 */
export interface LinkTestEngine {
  onupdate: ((sample: LinkTestSample) => void) | null
  onend: ((aborted: boolean) => void) | null
  setParameter: (name: string, value: unknown) => void
  setSelectedServer: (server: LinkTestServer) => void
  start: () => void
  abort: () => void
}

type SpeedtestConstructor = new () => LinkTestEngine

/**
 * The one test point there is: this router, reached through the origin the page
 * was served from. The engine appends the four endpoint names to `server`.
 */
const LINK_TEST_SERVER: LinkTestServer = {
  name: 'Open Linux Router',
  server: `${LINK_TEST_PREFIX}/`,
  dlURL: 'download',
  ulURL: 'upload',
  pingURL: 'ping',
  getIpURL: 'ip',
}

const engineScript = `${LINK_TEST_PREFIX}/speedtest.js`

let loading: Promise<SpeedtestConstructor> | null = null

/**
 * Load the engine's script once and hand back its constructor.
 *
 * A failed load is retryable: the viewer cookie behind /link-test/ is minted per
 * visit, so the usual failure is an expired session rather than a broken box,
 * and the page's own Try again should be able to recover.
 */
export function loadLinkTestEngine(): Promise<SpeedtestConstructor> {
  if (loading) return loading
  loading = new Promise<SpeedtestConstructor>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = engineScript
    script.async = true
    script.onload = () => {
      const Engine = (window as unknown as { Speedtest?: SpeedtestConstructor }).Speedtest
      if (Engine) resolve(Engine)
      else reject(new Error(`${engineScript} did not define the Speedtest engine`))
    }
    script.onerror = () => reject(new Error(`could not load ${engineScript}`))
    document.head.appendChild(script)
  })
  loading.catch(() => { loading = null })
  return loading
}

/** The router's own test settings. Telemetry stays off there, not here. */
export async function loadLinkTestSettings(): Promise<Record<string, unknown>> {
  const response = await fetch(`${LINK_TEST_PREFIX}/settings.json`, { headers: { Accept: 'application/json' } })
  if (!response.ok) throw new Error(`the test settings answered ${response.status}`)
  const settings: unknown = await response.json()
  if (!settings || typeof settings !== 'object' || Array.isArray(settings)) {
    throw new Error('the test settings are malformed')
  }
  return settings as Record<string, unknown>
}

/** Build an engine that will measure this router, ready to start. */
export function createLinkTestEngine(Engine: SpeedtestConstructor, settings: Record<string, unknown>): LinkTestEngine {
  const engine = new Engine()
  for (const [name, value] of Object.entries(settings)) engine.setParameter(name, value)
  engine.setSelectedServer(LINK_TEST_SERVER)
  return engine
}

/**
 * Start the test.
 *
 * The engine builds `speedtest_worker.js` relative to the document, which on
 * this page would be /tools/speedtest_worker.js. Point the Worker constructor at
 * the test surface for the duration of start() and put the global back
 * afterwards: nothing else on this page creates workers.
 */
export function startLinkTest(engine: LinkTestEngine): void {
  const NativeWorker = window.Worker
  const root = new URL(`${LINK_TEST_PREFIX}/`, window.location.origin).toString()
  window.Worker = function (url: string | URL, options?: WorkerOptions) {
    return new NativeWorker(new URL(String(url), root), options)
  } as unknown as typeof window.Worker
  try {
    engine.start()
  } finally {
    window.Worker = NativeWorker
  }
}

/**
 * The settled numbers of a finished sample.
 *
 * Null means the run did not measure something usable: the worker reports a
 * failed direction as "Fail" and an unmeasured value as an empty string, and
 * neither may become a zero. A real zero is only impossible for the two
 * transfers, whose worker reports them in whole Mbps and which get no data at
 * all when they fail.
 */
export function linkTestResult(sample: LinkTestSample): LinkTestResult | null {
  const latency = measured(sample.pingStatus)
  const download = measured(sample.dlStatus)
  const upload = measured(sample.ulStatus)
  if (!Number.isFinite(latency) || !Number.isFinite(download) || !Number.isFinite(upload)) return null
  if (latency < 0 || download <= 0 || upload <= 0) return null
  const jitter = measured(sample.jitterStatus)
  return { latency, download, upload, jitter: Number.isFinite(jitter) ? jitter : null }
}

/** One worker value as a number, or NaN when the worker did not measure it. */
function measured(raw: string): number {
  const value = raw.trim()
  if (value === '' || value === 'Fail') return Number.NaN
  return Number(value)
}
