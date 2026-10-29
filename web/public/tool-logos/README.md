# Tool project artwork

Bundled locally so router pages do not request third-party images at runtime.
Brand marks identify their respective projects; they do not imply endorsement.

A diagnostic tile wears a project's mark only when the tile names that project.
A tool that measures through a protocol, or through OLR's own code, keeps its
initial on a tinted tile instead of borrowing a vendor's mark. The internet
speed tests are the one case where a page does name the service it measures
through — Speedtest by Ookla, Cloudflare Speed Test and Fast.com are three
pages, not one page and a picker — so each wears its service's mark.

Most of these come from [theSVG](https://thesvg.org) (MIT; `public/icons/<slug>/`
in GLINCKER/thesvg), the same library behind the Access marks and the selectable
service icons. The dependency is pinned, so the files below are what
`@thesvg/icons` 3.4.2 carries; a bump should be re-checked against them.

- Speedtest: `speedtest/default.svg`, in the library's own `#141526`.
- Cloudflare: `cloudflare/mono.svg` filled with the library's own `#F38020`. The
  default variant is a white-and-orange cloud drawn for a dark page, whose white
  body disappears on the launcher's white tile.
- LibreSpeed: `librespeed/default.svg` (LGPL-3.0), which brings its own
  near-white rounded background, so the launcher draws it edge to edge.
- Fast.com: theSVG carries no fast.com entry, and Netflix's mark there belongs to
  the parent brand rather than to this site, so `fastcom.png` is fast.com's own
  app icon — `https://fast.com/assets/favicons/apple-icon-180x180.png`, the red
  gauge Netflix serves for the page. It is the unmodified file; the white tile
  supplies the padding.
- NextTrace: [`assets/logo.png`](https://github.com/nxtrace/NTrace-core/blob/c7e663fa994120791f0545124884f06e6aeb2d38/assets/logo.png)
  from the NextTrace project (GPL-3.0), downloaded at commit `c7e663f` and
  resized to 256px; the artwork itself is unchanged. theSVG carries no NextTrace
  entry, so the project's own file is the source. The mark already has a rounded
  background, so the launcher draws it edge to edge rather than in a white tile.
- iperf3: the iperf3 project's own lockup from
  [`software.es.net/iperf`](https://software.es.net/iperf/), supplied as a
  1080×608 image with a `#30333b` rounded plate. theSVG carries no iperf3 entry.
  The swoosh, wordmark, colours and proportions are unchanged; the plate is
  extended above and below the lockup so the mark fills the square launcher
  tile, and the file is `iperf3.png` at 256px.

Trademarks remain with their owners. No launcher artwork is drawn or recoloured
locally; iperf3's plate is only extended to square the tile.
