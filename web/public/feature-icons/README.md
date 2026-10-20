# Feature app icons

Original artwork generated with the built-in image tool (gpt-image-2) and
reviewed by the product owner. These identify OLR functions, not third-party
projects. They are bundled locally; pages make no image-generation or external
image requests at runtime.

The approved style is a rounded app tile with a gentle color field and a bold,
simple foreground made from flat color blocks. Use dark slate and warm ivory,
clear functional subjects, generous spacing and no lettering. Avoid 3D,
materials, bevels, highlights, cast shadows and decorative branding.

| File | Approved source filename | Subject / final prompt brief |
| --- | --- | --- |
| `port-forwarding.webp` | `edited-port-forwarding-flat-color-blocks-c7a26e.png` | Warm orange tile; flat slate port ring crossed by a broad ivory route and forward arrow. |
| `socks5.webp` | `edited-socks5-app-flat-port-standard-254765.png` | Blue tile; an ivory route between two endpoints passing through a central slate relay. Match the Port forwarding reference's flat color blocks. |
| `http.webp` | `edited-http-simple-globe-v2-f0643b.png` | Sky-blue tile; one slate globe with broad ivory meridian/equator bands. Remove browser window, pointer and additional objects. |
| `https.webp` | `edited-https-simple-lock-v2-df0e6f.png` | Sage tile; one ivory padlock with slate shackle and keyhole. Remove globe and additional objects. |

SOCKS5 is used in both Access directions. Port forwarding is used in Advanced.
HTTP and HTTPS are retained as approved assets for future reuse: the current
directory has no HTTP tile, and HTTPS publishing uses Caddy's existing brand
mark. Adding these files does not introduce protocol support or new tiles.

The generated source images remain in the review session. Runtime files remove
the outer white field, trim the artwork to its own app tile and encode a 256px
WebP using the existing build utility:

```sh
node web/scripts/optimize-icons.mjs <approved-png-directory> \
  --out web/public/feature-icons --size 256 --fill 1 --mass 1 --quality 0.9
```

This preserves the approved composition, clears the outside corners for dark
themes and avoids shipping megabyte-sized generation masters in the router.
