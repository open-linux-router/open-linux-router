# Access UI

Access groups available connections by direction. Its landing page shows a short
description and current state for each capability using the shared grouped list.
Remote access, HTTPS publishing, port forwards and external SOCKS5 settings live
under `/access`. Their former Advanced addresses redirect while preserving query
parameters and fragments. Ways out remains in Gateway because it assigns routing
policy to networks and devices; Access labels this cross-link explicitly.

The landing page reads existing configuration and status endpoints. It never
applies network changes. A failed observation is shown as unavailable rather than
off, and saved intent with a stopped service is shown as needing attention. The
outbound SOCKS5 summary describes the local TUN service, not the reachability of
the external proxy. Protocol settings and configuration apply paths remain on
their respective pages.

## Validation

UI lint, production build and all UI tests pass with Node 24, including seven
Access status regression tests. Linux Go vet and tests pass with Go 1.27 (the
commands behind `make check` were run directly because make was unavailable).
Browser checks use synthetic API responses: 375, 768, 1024 and 1440 pixel widths,
phone landscape, light/dark themes, keyboard navigation, enlarged text, loading
and failure states, all child-page back links and nine legacy redirects. No live
router settings were changed.

## Illustration

`web/src/assets/access-connections.webp` is a decorative, transparent 256 × 256
WebP (8,690 bytes), displayed at 64–96 CSS pixels with empty alternative text.
It was generated using the built-in MyLifeDB `generate_image` tool with
`gpt-image-2`, medium quality and a square size request, then resized with Pillow
and encoded at quality 88. Its alpha channel is preserved. It is a page
illustration, not a replacement for the application's logo.

Final generation prompt:

```text
Use case: stylized-concept
Asset type: compact decorative illustration for Open Linux Router's Access settings page, displayed at 96px beside a short introduction, not a hero banner or replacement brand logo.
Primary request: a refined physical sculpture suggesting private connections into and out of a home network: two interlocking open rounded loops, one satin porcelain silver and one restrained system blue, with two tiny connection nodes.
Scene/backdrop: genuinely transparent background.
Style/medium: premium minimalist 3D product illustration, precise smooth curves, matte ceramic, soft studio lighting, subtle contact shadows, crisp silhouette.
Composition/framing: centered small balanced sculpture occupying 80 percent of square frame, readable at small scale. Quiet and functional, suitable on both white and charcoal UI backgrounds.
Color palette: porcelain gray and muted medium blue matching a neutral system-style control panel.
Constraints: transparent alpha; no text, no letters, no watermark, no gradients in backdrop, no frame, no glow, no additional objects or wires.
```
