# Bundled brand marks

`logos.bin` and `logos.idx` are the searchable logo library the device and site
pickers offer, generated from [theSVG](https://thesvg.org) (MIT;
`@thesvg/icons`, `public/icons/<slug>/default.svg` in GLINCKER/thesvg) and
embedded in `olrd` by `internal/brandlogo`.

They exist because the picker used to request each mark from
`raw.githubusercontent.com` while drawing a page — a third-party dependency for
a router's own UI, and a pending spinner whenever the browser had no route to
GitHub. The artwork is identical for every installation, so it ships in the
binary with the device pictures and the OS marks.

## Generated, not committed

Both files are build output and are gitignored. The library is artwork, not
code: a 6 MB gzip blob in the tree tracks nothing a version bump does not, so
the source of truth is `@thesvg/icons` in `web/package.json`, pinned exactly.

```
cd web && npm install      # once, or `make web-deps`
make web                   # or `npm run build`, which runs the generator first
make brand-logos           # regenerate the marks alone, without the SPA
```

Bumping the library is `npm install --save-dev --save-exact @thesvg/icons@<v>`
and a rebuild; the marks change and the diff does not.
`web/scripts/build-brand-logos.mjs` does the packing and refuses a mark carrying
a script, a `foreignObject` or an inline event handler rather than filtering it
at request time. It also drops the XML prolog and any DOCTYPE, the one remaining
thing in the library that names an address off the box.

## What ships

| | |
|---|---|
| Marks | 6,418 in `@thesvg/icons` 3.2.4 |
| SVG as published | 13.9 MiB |
| On the binary, per-mark gzip | 5.9 MiB |

`logos.bin` is one gzip stream per mark, concatenated in slug order; `logos.idx`
is `slug<TAB>offset<TAB>length`, sorted by slug. Serving a mark is a slice of
`logos.bin` handed to the browser as `Content-Encoding: gzip` — the router never
decompresses anything to draw a page. The index is what the search endpoint
scans, so the SPA does not download six thousand slugs to filter in a text box.

A `go build` that never ran the web build has no catalogue at all: it still
compiles, the daemon warns at startup, the endpoints answer `503`, and the
pickers fall back to category pictures. That is the same trade as the SPA
itself, which needs `make web` to exist.

Brand marks identify their owners and do not imply endorsement; the trademarks
remain with them. File a mark this library should not carry as a bug.
