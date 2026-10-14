# World traffic

Overview's fifth card uses MapLibre with a bundled Natural Earth 1:110m
country map (public domain). It makes no requests to a tile provider.
DB-IP's existing local country/ASN databases locate only the top ten IPs;
unknown locations remain in the ranking and its coverage denominator.
Country label positions are representative points, not city coordinates.

The daemon samples IPv4/IPv6 conntrack counters every two seconds, shared
across browsers. It retains counter differences over up to thirty seconds
in memory, aggregates by remote IP, and selects ten by combined upload and
download. Ranking has deterministic IP tie-breaking. Rates are bytes/second
over the available window; the first sample is a baseline. Coverage is the
top ten's share of all observed eligible remote-IP traffic, not a share of
the separate nftables interface/device totals.

Blue indicates download, teal upload. Separate curved paths carry directional
particles; logarithmic line widths cap visual extremes. Reduced motion
disables particles. Remote countries matching the router country have a point
rather than a fabricated inter-city arc. The details dialog exposes IPs and
both rates, unavailable states, sample duration and GeoIP attribution.

Gateway traffic counting enables `net.netfilter.nf_conntrack_acct=1` through
the normal apply plan. This affects newly created connections; older entries
may lack counters until they expire. Turning counting off stops this sampler
and clears its window, but does not disable the shared kernel facility used
by remote access. No connection metadata is persisted by this sampler.

Measurements include router-originated and forwarded connections with one
local endpoint and one public remote endpoint. Private, link-local and CGNAT
destinations are excluded. Conntrack original/reply counters are translated
according to endpoint ownership, including inbound connections. Short-lived
connections that disappear between samples, final bytes of expired flows,
and traffic bypassing kernel accounting can be missed. VPN endpoints are
displayed as observed; inner destinations are not inferred.

Large connection tables still cost a full netlink dump, parsing and memory
per sample. Measure this on target hardware before reducing the interval.
The map's top-ten coverage does not claim exact whole-router accounting.
