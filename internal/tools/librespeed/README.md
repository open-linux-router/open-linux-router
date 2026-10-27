# LibreSpeed (vendored)

LibreSpeed's browser speed test, unmodified, served by `olrd` for the
device-to-router tool (`internal/tools/linktest.go`).

- Upstream: https://github.com/librespeed/speedtest
- Version: 6.3.1, commit `06524abd22e4581fb38a32393b80f37beec05f34`
- Licence: GNU LGPLv3 — see `LICENSE`, which is upstream's own file.

## What is here

`static/` is upstream's browser-side code as published:

| From upstream | Files |
| --- | --- |
| `frontend/` | `index.html`, `javascript/`, `styling/`, `fonts/`, `images/` |
| repository root | `speedtest.js`, `speedtest_worker.js`, `stability.html`, `stability_worker.js` |

Dropped on purpose, because the router does not serve them: the frontend's
README and screenshots, `images/background-original.jpeg` (the stylesheet uses
the compressed `background.jpeg`), and every server-side file (`backend/*.php`,
`results/`, the telemetry database schemas). Nothing in `static/` has been
edited: the four endpoints the worker calls are implemented in Go and pointed at
by the `server-list.json` and `settings.json` that `linktest.go` generates at
request time.

That is deliberate. Keeping the upstream files byte-identical is what makes a
future upgrade a copy rather than a merge, and it keeps the LGPL half of this
directory separable from the rest of the binary at the file level.

OLR's tool page loads `speedtest.js` and starts `speedtest_worker.js` in its own
document; it does not frame upstream's `index.html`, which stays served for a
direct visit. The page around the engine — start control, running state and
result tiles — is OLR's own, and the engine's URLs are pointed at `/link-test/`
from `web/src/features/tools/link-test.ts`.

## Updating

1. Take the release tag, not `master`.
2. Copy the table above into `static/`, preserving paths.
3. Replace `LICENSE`.
4. Update the version and commit here and in `web/src/routes/credits-data.ts`.
5. Open the tool page on a router and run one test: the test must start, run in
   that browser, and report latency, jitter, download and upload.
6. Re-check that `linktest_test.go` still finds `LibreSpeed` in `speedtest.js`
   and that `settings.json` still turns telemetry off — upstream renames things.
