# Device QoS: priority and speed limits

**Implementation:** Linux HTB/fq_codel queues enforce policies automatically,
without an enable switch. Downloads are shaped on managed LAN egress; uploads
redirect LAN ingress to owned IFBs. Ethernet MAC classifiers cover both address
families and NAT without changing gateway routing marks. Optional link rates
apply per LAN interface; without rates, priority only orders local contention.

Legacy `enabled` is ignored. API zero clears a limit. Pending policies lack an
unambiguous managed LAN interface; device IP changes do not affect classification.
Startup and ten-second reconciliation restore intent and detect drift. No-op
applies retain queues. Editing rebuilds queues and may discard queued packets.
Errors explicitly distinguish saved intent from successful kernel application.

Foreign roots/ingress filters are refused; unconfigured zero-handle kernel roots
(including default multi-queue NIC roots)
may be replaced. Root handle `4f4c:`, ingress preference 20290, and IFBs with
`olr:qos:<interface>` aliases identify ownership. Removing the last policy removes
owned roots/redirect filters but retains empty ingress attachments and IFBs.
nftables flowtables are refused. Proprietary acceleration must be disabled by
the operator. Unset aggregate rates use a 100 Gbit/s technical ceiling.

Run privileged traffic verification with `OLR_QOS_INTEGRATION=1 go test
./internal/qos -run TestKernelTraffic -v` as root, with `ip`, `tc`, `nft` and
`iperf3` installed. It tests real IPv4 NAT/IPv6 traffic in both directions and
reset in isolated network namespaces. Restricted workspaces run unit tests only.

## 1. What the operator sees

The device detail page exposes exactly two policies for traffic crossing this
router:

- **Network priority:** High, Normal (the default), or Low. Priority decides
  who gets served first when the managed link is busy. It is not a reserved
  minimum speed, and Low must be allowed to use spare capacity.
- **Speed limit:** optional maximum download and upload rates in Mbps, set
  independently. An unset direction is unlimited by this device policy; API zero
  clears the limit. The two limits are ceilings, not targets or
  promises of available bandwidth.

Show the saved policy and its *effective status* separately. A policy is not
"active" just because its configuration was stored. If device placement or a
kernel facility is missing, explain which one
prevents enforcement. Defaults are Normal and unlimited; no policy changes any
packet until a device policy is configured.

A single-device edit must not rewrite the entire device inventory. The policy
belongs to `qos` and refers to the canonical device MAC owned by `devices`.
It does not become an identity field in `devices`, nor does it depend on DHCP:
a device may have a static address. Its detail page is an entry point to the
QoS API, not the owner of the policy.

## 2. Link setup (not another per-device policy)

The router needs configured upload and download *shaping rates*, based on
measured throughput rather than the physical Ethernet port speed. Otherwise
priority cannot reliably control the bottleneck in an ISP modem or upstream
network. Provide one optional setup surface for these rates; blank is unlimited.
The Overview page's browser-local utilization limits are display preferences;
they must not silently become router configuration or imply that QoS is on.

Shape on the managed LAN interface where Ethernet identities remain available.
This also limits routed LAN-to-LAN traffic. Aggregate rates are independent per
LAN; a shared whole-WAN ceiling across several LANs is not implemented.
Since downloads have already crossed the ISP link, download
shaping manages the subsequent queue and TCP feedback; it cannot prevent bytes
from having arrived at the router. Report both directions independently.

## 3. Kernel ownership and classification

`qos` owns only its qdiscs, filters, IFB and classifier rules. It must inspect
existing root qdiscs before writing, refuse a foreign owner, and remove only
objects it can identify as its own. The kernel state is observed fresh for
plan/drift/status; a daemon restart restores stored intent without replacing a
correct running queue. Adding, editing or clearing a policy may
interrupt queued packets; link rate changes have UI confirmation. Report
partial failures rather than claiming a transaction.

Use Linux `tc` for shaping and queueing. A classful hierarchy (for example HTB
with fq_codel leaves) can express per-device ceilings and High/Normal/Low
scheduling while allowing unused capacity to be borrowed. CAKE is useful for
whole-link SQM but by itself is not a drop-in implementation of arbitrary
per-device ceilings. Do not promise per-application classification: the two
controls here classify *devices*. Existing gateway fwmarks select exits;
QoS must neither overwrite those bits nor mistake an exit mark for a priority.

Resolve each MAC's managed LAN interface through `devices` at apply time and
reconcile placement changes. Ambiguous or unknown placement leaves the policy
pending. Classifiers match MACs, so address changes need no rule rewrite.
Cover both directions and both address families in tests. Locally
switched traffic that does not traverse this router is outside scope. Detect
hardware/software flow offload that bypasses the queue and do not report an
unenforced policy as active.

## 4. Acceptance checks

1. With no device policies, existing traffic and foreign qdiscs are untouched.
   With measured rates configured, filling the managed queue no longer makes a high-priority flow wait
   behind a low-priority bulk transfer; an idle low-priority device can use
   spare capacity.
2. An upload/download limit caps only that device in that direction, including
   after its address changes. Removing a limit restores unrestricted use up to
   the managed link rate; changing a name, icon or group preserves the policy.
3. A reboot restores the policy. A no-op daemon restart does not drop a queue.
   Clearing policies removes only olr-owned state.
4. Missing kernel support, an occupied qdisc, unknown LAN placement or
   a partial apply is visible in status and does not yield a false success in
   the UI. Verify IPv4, IPv6, NAT, gateway exits and VPN egress separately.
5. The API and CLI expose the same two per-device concepts as the detail page.
   The UI labels Mbps consistently and validates positive, bounded numbers.

Implementation should be exercised in Linux network namespaces with traffic
and latency measurements, not just a mock of the `tc` calls. A Go unit test on
another OS cannot establish that the policies actually shape packets.
