# Device artwork generation

These three operator-selected illustrations use the existing front-view, soft-lit,
simplified device style. They are 256×256 transparent WebP assets, framed by
`web/scripts/optimize-icons.mjs`; their masters remain in the image-generation session.
They are explicit picker choices, never inferred from a vendor or hostname.

The router images were restyled from the manufacturer references below with the
built-in image editing tool. The NUC is a text-generated illustrative variant:
the PCMag page blocked access and no usable product photo was retrieved. It is
not a photo-verified Tier 2 model image. Its name identifies the intended device,
but its small port details should not be treated as an exact product depiction.

## References

- NUC: https://www.pcmag.com/reviews/intel-nuc-12-extreme-kit-dragon-canyon
- Xiaomi: https://www.mi.com/xiaomi-routers/be-7000
  - Reference: https://cdn.cnbj1.fds.api.mi-img.com/product-images/xiaomi-routers/be-70007ff954/section-01.jpg
- TP-Link: https://www.tp-link.com.cn/product_2012.html
  - Reference: https://www.tp-link.com.cn/content/images/detail/2012/TL-XDR6080Turbo-new1.jpg

## Final prompts

### Intel NUC 12 Extreme

Use case: precise-object-edit. Edit target: the supplied existing device icon. Correct ONLY the proportions of this Intel NUC 12 Extreme Dragon Canyon front-view device illustration. Current silhouette is too tall and skinny. Make front chassis height exactly 1.575 times its WIDTH (189 mm high / 120 mm wide), excluding tiny feet. This is a SHORT BROAD COMPACT mini tower, not a tall tower. Shorten the large blank upper front panel substantially, keep chassis width substantial. Retain graphite black color, chamfered corners, two small bottom USB ports, headphone socket, power button, tiny feet, front view with camera barely above, soft even light, simplified semi-realistic phone settings device thumbnail style. Single centered device on pure white background, no shadow, no ground plane, no logos, no skull, no text, no lights, no additional details. Square canvas with ample margin. Critical: front panel total height / width = 1.575, never 2 or greater. Do not merely zoom out: change the chassis geometry to be shorter and broader.

### Xiaomi BE7000

Use case: style-transfer. Restyle ONLY the Xiaomi BE7000 router in the supplied manufacturer photograph into our existing device icon set. Clean device thumbnail illustration, like the device pictures in a phone's settings app. Shown straight-on from the front, perfectly symmetrical, upright, camera very slightly above. Clean simplified semi-realistic rendering, soft even lighting, crisp edges, designed to stay legible at 32 pixels. Single object centred, filling about 80% of the frame, plain pure white background, no shadow, no ground plane. Preserve the real device's shape and proportions: wide low matte charcoal-black wedge body, broad sloping upper front face, softly rounded outer top corners, three simple horizontal dark ribs at base, exactly six tall flat upright dark antennas arranged symmetrically, outer pair at the sides and four behind body. Keep every antenna clearly visible and separated in frontal view. Remove blue lighting, all surroundings, reflections, text, NFC mark, logos, status LEDs and gold lettering. Neutral graphite materials. Very minimal: a few large simple forms, no fine textures, no cables, no small parts. This device has no screen.

### TP-Link TL-XDR6080 Turbo

Use case: style-transfer. Restyle ONLY the router in the supplied manufacturer photograph into the project's device thumbnail set. Clean device thumbnail illustration, like the device pictures in a phone's settings app. Shown straight-on from the front, perfectly symmetrical, upright, camera very slightly above. Clean simplified semi-realistic rendering, soft even lighting, crisp edges, designed to stay legible at 32 pixels. Single object centred, filling about 80% of the frame, plain pure white background, no shadow, no ground plane. Preserve the real TP-Link TL-XDR6080 Turbo silhouette: low wide rectangular body, silver central top panel and silver front, dark charcoal ribbed side wings, exactly eight tall flat upright charcoal antennas arranged symmetrically four on either side with separation visible. Preserve real proportions and neutral silver/black materials but simplify fine ribbing. Remove all promotional surroundings, lettering, logos, brand marks and LEDs. No cables, no screen. Very minimal: a few large simple forms, no fine textures, no small parts. CRITICAL CORRECTION: the previous output had only SIX antennas. Must have EIGHT, FOUR visibly separated on the LEFT and FOUR visibly separated on the RIGHT, all exactly the same height, no overlaps; count 1 2 3 4 left, 5 6 7 8 right. Simplify side fins into three broad grooves. Camera only very slightly above so silver top depth is modest.
