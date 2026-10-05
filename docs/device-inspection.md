# Temporary device request inspection

The device detail page shows DNS queries separately from HTTP(S) requests. A DNS lookup is not proof of a visit. The DNS list is a recent network-wide sample matched to the device's current IPs; the relay's query log must be enabled.

Request inspection is opt-in and temporary. Install `mitmproxy` (`mitmdump`) on the Linux router before starting. On Debian, use `sudo apt install mitmproxy`. It is not a package dependency because it is only needed for debugging. OLR starts its own transparent mitmdump instance and creates a debugging CA in `/var/lib/open-linux-router/inspection` (directory mode 0700). Back up or remove this directory only with the implications for devices that trust its CA in mind. The API exposes only the public PEM, never the private key.

1. Open an online device's detail page and start a 15-minute inspection. The first session generates the CA.
2. Download the public CA from the page and install it **on that device** as a trusted root certificate for HTTPS. Follow the OS's certificate trust instructions; installing without enabling trust may not suffice. There is no proxy address to configure.
3. Browse from that device. HTTP requests need no CA; HTTPS requests appear only if the app accepts the user CA. Stop and clear when finished, and remove the CA from the device's trust store. Stopping interception does **not** revoke trust on the device.

Only one device and one unique, currently observed private IPv4 address are supported. Multiple addresses or IPv6 cause startup to refuse rather than silently intercept only part of a device. OLR redirects only TCP 80/443 to public destinations; UDP/443 (QUIC), local/LAN destinations, IPv6, other ports and non-HTTP protocols are not inspected. OLR does not block UDP/443 to force fallback. Certificate pinning and apps with separate trust stores may fail to connect or remain uninspected. The page does not claim a complete connections list.

The proxy must be ready before nftables interception is installed. OLR checks the device's identity throughout the session, removes its own nftables table at stop/timeout/proxy exit and before startup, and restores normal forwarding on failure. The session holds at most 200 request/failure events in daemon memory, including headers but not bodies, and clears them on stop. Treat headers and URLs as sensitive. CA trust and the CA key remain after a session; a future rotation workflow should coordinate removing trust from devices.

Validation on a real Linux router is required before relying on this in production: test HTTP and trusted HTTPS, an untrusted/pinned app, IPv4 address changes, QUIC, proxy crash, daemon restart and nft cleanup failure. macOS Go/UI tests cannot exercise the netfilter path.
