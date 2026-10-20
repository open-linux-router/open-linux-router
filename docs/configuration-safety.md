# Configuration safety

The global apply lock covers reading intent, merging a PATCH or editing an
object, validating cross-module references, planning and saving/applying the
result. Bodies are read before taking the lock. GET and plan requests remain
independent of the lock.

The DHCP/DNS background recovery watch uses the same lock. Its initial poll is
only a hint; it rechecks eligibility after acquiring the lock, then reads and
applies current intent without releasing it. An intervening disable, file edit
or disruptive change is therefore not overridden by a stale recovery decision.
Cancelled lock waits and failed eligibility reads do not consume restart attempts.

Config GET responses include a strong `ETag`. Send it as `If-Match` when
replacing a fetched document or array. A concurrent change returns 412 before
anything is written. Reload and review the draft; do not silently retry it
against the new version. This is optional for older clients. The browser
attaches the revision to the fetched config and preserves it through object
spread, so a later background refresh cannot authorize an older draft.

Device identity edits use `PUT /api/devices/devices/{mac}` and forgetting uses
`DELETE` at the same path. These edit one stored identity under the lock and
preserve all other devices and groups. Observed presence and DHCP/firewall
policies are separate resources. Whole-inventory PUT remains available for
imports, with `If-Match` recommended.

Firewall native rules retain their existing combined runtime/intent revision
from `/ruleset`; `/config` also accepts the intent ETag for ordinary config
edits. Native rule editors should continue using the combined revision to
detect changes made outside olr.

Link, dial/WAN, DHCP and DNS config writes accept `dry_run` and `confirm`.
Their execution plan is checked under the lock before saving intent or changing
runtime state. A disruptive request without `confirm=true` returns 409 with
the plan and no completed steps. A dry run returns 200 and writes nothing.
Malformed flags return 400. The browser sends confirmation only after its
confirmation dialog; the local CLI sends it explicitly, matching the existing
gateway CLI contract. Use `--dry-run` to review CLI changes first.

`POST /apply` repairs previously stored intent without another confirmation.
Disruptive link, uplink, DHCP, DNS, gateway, firewall and WireGuard changes
save a durable recovery point before applying. Explicitly keep the change in
the browser within two minutes, or use `olr recovery status` followed by
`olr recovery confirm <id>`. A successful apply or reconnect never confirms
automatically. `olr recovery revert <id>` restores immediately. While a trial
is pending, unrelated configuration edits are refused, including from local CLI.
Multi-request browser edits continue the same trial using the
`X-OLR-Recovery-ID` response/request header, without extending its deadline.

Timeout restores the previous complete intent and reconciles managed network
state, including retiring addresses/routes introduced by the trial. Existing
IPv4 addresses and main-table default routes are captured for first-adoption
recovery. The mode-0600 journal in `/var/lib/open-linux-router/connectivity.json`
survives restart; startup reverts an unconfirmed trial immediately. Recovery
failures remain visible and retry, preserving both the previous and trial intent.
Physical link failure, hardware removal, external configuration changes and
unavailable backends can still require console access. Recovery does not undo
external package installations or changes made outside OLR.
