# Device QoS: priority and speed limits

## 1. What the operator sees

The device detail page exposes exactly two policies for traffic crossing this
router:

- **Network priority:** High, Normal (the default), or Low. Priority decides
  who gets served first when the managed link is busy. It is not a reserved
  minimum speed, and Low must be allowed to use spare capacity.
- **Speed limit:** optional maximum download and upload rates in Mbps, set
  independently. An unset direction is unlimited by this device policy; zero
  is not a synonym for unlimited. The two limits are ceilings, not targets or
  promises of available bandwidth.

Show the saved policy and its *effective status* separately. A policy is not
"active" just because its configuration was stored. If the uplink, a device
address, a kernel facility, or a shaping rate is missing, explain which one
prevents enforcement. Defaults are Normal and unlimited; no policy changes any
packet until QoS is explicitly enabled.

A single-device edit must not rewrite the entire device inventory. The policy
belongs to `qos` and refers to the canonical device MAC owned by `devices`.
It does not become an identity field in `devices`, nor does it depend on DHCP:
a device may have a static address. Its detail page is an entry point to the
QoS API, not the owner of the policy.

## 2. Link setup (not another per-device policy)

The router needs configured upload and download *shaping rates*, based on
measured throughput rather than the physical Ethernet port speed. Otherwise
priority cannot reliably control the bottleneck in an ISP modem or upstream
network. Provide one setup surface for these rates, with a clear off state.
The Overview page's browser-local utilization limits are display preferences;
they must not silently become router configuration or imply that QoS is on.

Start with one owned WAN egress from `dial` and explicitly refuse unsupported
multi-WAN or unmanaged-uplink cases, rather than guessing which interface to
shape. Apply upload shaping to WAN egress. Redirect WAN ingress through an IFB
for download shaping. Since ingress has already crossed the ISP link, download
shaping manages the subsequent queue and TCP feedback; it cannot prevent bytes
from having arrived at the router. Report both directions independently.

## 3. Kernel ownership and classification

`qos` owns only its qdiscs, filters, IFB and classifier rules. It must inspect
existing root qdiscs before writing, refuse a foreign owner, and remove only
objects it can identify as its own. The kernel state is observed fresh for
plan/drift/status; a daemon restart restores stored intent without replacing a
correct running queue. Enabling, changing the link rate or disabling QoS may
interrupt queued packets, so use the normal plan/confirmation path and report
partial failures rather than claiming a transaction.

Use Linux `tc` for shaping and queueing. A classful hierarchy (for example HTB
with fq_codel leaves) can express per-device ceilings and High/Normal/Low
scheduling while allowing unused capacity to be borrowed. CAKE is useful for
whole-link SQM but by itself is not a drop-in implementation of arbitrary
per-device ceilings. Do not promise per-application classification: the two
controls here classify *devices*. Existing gateway fwmarks select exits;
QoS must neither overwrite those bits nor mistake an exit mark for a priority.

Resolve MACs to currently observed IPv4/IPv6 addresses through `devices` at
apply time and reconcile lease/neighbour changes. If an address is ambiguous or
unknown, say the device's policy is pending rather than applying it to another
client. Cover both directions and both address families in tests. Locally
switched traffic that does not traverse this router is outside scope. Detect
hardware/software flow offload that bypasses the queue and do not report an
unenforced policy as active.

## 4. Acceptance checks

1. With QoS off, existing traffic and foreign qdiscs are untouched. With it
   on, filling the uplink no longer makes an unrelated high-priority flow wait
   behind a low-priority bulk transfer; an idle low-priority device can use
   spare capacity.
2. An upload/download limit caps only that device in that direction, including
   after its address changes. Removing a limit restores unrestricted use up to
   the managed link rate; changing a name, icon or group preserves the policy.
3. A reboot restores the policy. A no-op daemon restart does not drop a queue.
   Disabling QoS removes only olr-owned state.
4. Missing kernel support, an occupied qdisc, unknown address, an absent WAN or
   a partial apply is visible in status and does not yield a false success in
   the UI. Verify IPv4, IPv6, NAT, gateway exits and VPN egress separately.
5. The API and CLI expose the same two per-device concepts as the detail page.
   The UI labels Mbps consistently and validates positive, bounded numbers.

Implementation should be exercised in Linux network namespaces with traffic
and latency measurements, not just a mock of the `tc` calls. A Go unit test on
another OS cannot establish that the policies actually shape packets.
