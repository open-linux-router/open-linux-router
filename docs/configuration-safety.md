# Configuration safety

The global apply lock covers reading intent, merging a PATCH or editing an
object, validating cross-module references, planning and saving/applying the
result. Bodies are read before taking the lock. GET and plan requests remain
independent of the lock.

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
This confirmation gate does not provide automatic rollback after losing
connectivity; console access is still needed for deliberate address changes.
