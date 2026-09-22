package dial

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC) }

// docs/ddns.md §6: the three questions stay separate. The state this asserts on
// is the one the whole module is arranged around — the address is fresh, and the
// record at the provider has been stale for four days.
func TestStatusSaysAllThreeThingsWhenPublishingIsBroken(t *testing.T) {
	published := now().Add(-96 * time.Hour)
	checked := now().Add(-30 * time.Second)

	view := recordView{
		Name: "home.example.net", Provider: "cloudflare",
		Source: SourceReflector, From: "https://reflector.example/", Watched: true,
	}
	viewState(&view, RecordState{
		Checked:          checked,
		Address:          "203.0.113.9",
		Published:        published,
		PublishedAddress: "198.51.100.1",
		PublishError:     "Invalid API Token (code 9109)",
		Failures:         3,
		Retry:            now().Add(time.Hour),
	})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{view}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	for _, want := range []string{
		"203.0.113.9",                  // what the address is
		"30s ago",                      // when it was read
		"4 days ago at 198.51.100.1",   // when it was last published, and as what
		"Invalid API Token",            // the provider's own words
		"backing off after 3 refusals", // and what happens next
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status does not say %q:\n%s", want, got)
		}
	}
}

// The row that must not read as "pending".
func TestStatusDistinguishesNeverPublishedFromNotYet(t *testing.T) {
	refused := recordView{Name: "a.example.net", Watched: true}
	viewState(&refused, RecordState{Checked: now(), Address: "203.0.113.9", PublishError: "badauth"})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{refused}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "never — the provider refused: badauth") {
		t.Errorf("a record that has never published and is being refused reads as pending:\n%s", out.String())
	}

	pending := recordView{Name: "b.example.net", Watched: true}
	viewState(&pending, RecordState{})
	out.Reset()
	if err := writeStatusText(&out, statusResponse{Records: []recordView{pending}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "refused") {
		t.Errorf("a record that has not been checked yet reads as a failure:\n%s", out.String())
	}
}

// §3.2's diagnosis has to be a sentence, not a boolean somebody has to interpret.
func TestStatusExplainsCGNAT(t *testing.T) {
	view := recordView{Name: "home.example.net", Watched: true}
	viewState(&view, RecordState{
		Checked: now(), Address: "100.72.14.3", CGNAT: true,
		Published: now(), PublishedAddress: "100.72.14.3",
	})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{view}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "carrier-grade NAT") || !strings.Contains(got, "port forward") {
		t.Errorf("the CGNAT verdict does not explain itself:\n%s", got)
	}
}

// `omitempty` does nothing for a time.Time, so a record that has never been
// published would otherwise report the year 1 — a value every client has to
// know to special-case.
func TestStatusOmitsTimestampsThatNeverHappened(t *testing.T) {
	view := recordView{Name: "home.example.net", Watched: true}
	viewState(&view, RecordState{})

	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "0001-01-01") {
		t.Errorf("a zero timestamp reached the wire: %s", data)
	}
	for _, absent := range []string{"checked", "published", "retry"} {
		if strings.Contains(string(data), `"`+absent+`"`) {
			t.Errorf("%q is present for a record nothing has happened to: %s", absent, data)
		}
	}
}

// The credential cannot reach a diff an operator prints and pastes into a bug
// report.
func TestThePlanDiffIsRedacted(t *testing.T) {
	desired := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "cf-secret",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(Config{}, desired, testLinks(), Observed{})
	if len(plan.Changes) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if strings.Contains(plan.Changes[0].Diff, "cf-secret") {
		t.Fatalf("the token reached the diff:\n%s", plan.Changes[0].Diff)
	}
	if !strings.Contains(plan.Changes[0].Diff, RedactedToken) {
		t.Errorf("the diff does not show that a credential was set:\n%s", plan.Changes[0].Diff)
	}
}

// Adding a record does nothing to the box and starts this box talking to a
// third party on a timer. The plan is where an operator finds that out.
func TestThePlanSaysWhatAddingStarts(t *testing.T) {
	desired := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(Config{}, desired, testLinks(), Observed{})
	var joined string
	for _, w := range plan.Warnings {
		joined += w.Message + "\n"
	}
	if !strings.Contains(joined, "cloudflare") || !strings.Contains(joined, "reflector.example") {
		t.Errorf("the plan does not say who this box starts talking to:\n%s", joined)
	}
	if plan.Impact != impactNone {
		t.Errorf("Impact = %q; nothing on the box changes", plan.Impact)
	}
}

// Removing a record stops the updating; it does not remove the name. An
// operator who thinks otherwise leaves a stale public record behind.
func TestThePlanSaysRemovalLeavesTheNameBehind(t *testing.T) {
	stored := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(stored, Config{}, testLinks(), Observed{})
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != kindDelete {
		t.Fatalf("plan = %+v", plan)
	}
	var joined string
	for _, w := range plan.Warnings {
		joined += w.Message + "\n"
	}
	if !strings.Contains(joined, "stays at") {
		t.Errorf("the plan does not say the name survives:\n%s", joined)
	}
}

func TestAnEmptyPlanIsEmpty(t *testing.T) {
	stored := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}
	if plan := buildPlan(stored, stored, testLinks(), Observed{}); !plan.Empty {
		t.Errorf("planning a config against itself found changes: %+v", plan.Changes)
	}
}

// --- the uplink -------------------------------------------------------------

func uplinkConfig() Config {
	u := testUplink()
	return Config{Uplink: &u}
}

// The impact judges the *kernel*, not the stored config: setting up an uplink
// on a box whose distribution already provides a default route is a takeover
// and can drop the operator's session, even though the document went from
// nothing to something.
func TestTakingOverAnExistingDefaultRouteIsDisruptive(t *testing.T) {
	obs := Observed{
		Present: true, Up: true,
		Gateway:    netip.MustParseAddr("192.168.2.254"),
		GatewayDev: "enp1s0",
	}
	plan := buildPlan(Config{}, uplinkConfig(), testLinks(), obs)
	if plan.Impact != impactDisruptive {
		t.Errorf("impact = %q, want %q\n%+v", plan.Impact, impactDisruptive, plan.Changes)
	}
}

func TestProvidingAFirstDefaultRouteIsNotDisruptive(t *testing.T) {
	plan := buildPlan(Config{}, uplinkConfig(), testLinks(), Observed{Present: true, Up: true})
	if plan.Impact == impactDisruptive {
		t.Errorf("giving a box its first way out reported as disruptive\n%+v", plan.Changes)
	}
	if plan.Empty {
		t.Error("creating an uplink produced an empty plan")
	}
}

func TestMovingTheUplinkAddressIsDisruptive(t *testing.T) {
	before := uplinkConfig()
	after := before.Clone()
	after.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.2.10/24")

	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	if plan := buildPlan(before, after, testLinks(), obs); plan.Impact != impactDisruptive {
		t.Errorf("impact = %q, want %q", plan.Impact, impactDisruptive)
	}
}

// Nothing is torn down, so the impact is honestly none — and the warning is
// what stops "removed" reading as "this box is offline now".
func TestRemovingTheUplinkSaysNothingIsTornDown(t *testing.T) {
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	plan := buildPlan(uplinkConfig(), Config{}, testLinks(), obs)
	if plan.Impact != impactNone {
		t.Errorf("impact = %q; removing the uplink tears nothing down", plan.Impact)
	}
	if !hasWarning(plan, "after a reboot") {
		t.Errorf("nothing warns that the route stops being restored: %+v", plan.Warnings)
	}
}

// Drift: the document diff is empty by construction, so anything left is the
// kernel disagreeing with intent.
func TestAnUplinkPlannedAgainstADisagreeingKernelIsNotEmpty(t *testing.T) {
	stored := uplinkConfig()
	plan := buildPlan(stored, stored, testLinks(), Observed{Present: true, Up: true})
	if plan.Empty {
		t.Error("a box that lost its address and route read as up to date")
	}
}

func TestAnUplinkPlannedAgainstAnAgreeingKernelIsEmpty(t *testing.T) {
	stored := uplinkConfig()
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	if plan := buildPlan(stored, stored, testLinks(), obs); !plan.Empty {
		t.Errorf("an already-correct box produced %+v", plan.Changes)
	}
}

// Two managers on one interface is a state the operator can only fix if they
// can see it, and deleting what the other one put there is the failure the
// uplink object exists to stop.
func TestAForeignAddressOnTheUplinkIsCalledOut(t *testing.T) {
	obs := Observed{
		Present: true, Up: true,
		Addrs: []netip.Prefix{
			netip.MustParsePrefix("192.168.2.9/24"),
			netip.MustParsePrefix("192.168.2.77/24"),
		},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	plan := buildPlan(uplinkConfig(), uplinkConfig(), testLinks(), obs)
	if !hasWarning(plan, "192.168.2.77/24") {
		t.Errorf("the foreign address was not reported: %+v", plan.Warnings)
	}
	if !hasWarning(plan, "will not remove") {
		t.Errorf("the note does not say olr leaves it alone: %+v", plan.Warnings)
	}
}

func hasWarning(plan planView, substring string) bool {
	for _, w := range plan.Warnings {
		if strings.Contains(w.Message, substring) {
			return true
		}
	}
	return false
}

// The "configured and does not work" trap: source NAT hangs off a gateway exit,
// not off the route, so LAN traffic leaving through the default route has no
// way back. A plan note rather than a validation warning, because `dial` cannot
// read `gateway` and so cannot tell when the work is done — see
// unroutedNetworksNote.
func TestSettingAnUplinkSaysTheNetworksStillNeedAnExit(t *testing.T) {
	plan := buildPlan(Config{}, uplinkConfig(), uplinkLinks(), Observed{Present: true, Up: true})
	if !hasWarning(plan, "olr gateway add exit") {
		t.Fatalf("no note about the networks behind the router: %+v", plan.Warnings)
	}
	if !hasWarning(plan, "lan") {
		t.Errorf("the note does not name the network: %+v", plan.Warnings)
	}
}

func TestABoxWithNoNetworksIsNotToldAboutExits(t *testing.T) {
	links := uplinkLinks()
	links.Networks = nil

	plan := buildPlan(Config{}, uplinkConfig(), links, Observed{Present: true, Up: true})
	if hasWarning(plan, "olr gateway add exit") {
		t.Error("a box with no networks was told to route networks")
	}
}

// Said once, where the operator is looking at what they asked for — not on
// every drift check of a box that has not changed.
func TestAnUnchangedUplinkIsNotToldAboutExitsAgain(t *testing.T) {
	stored := uplinkConfig()
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	if plan := buildPlan(stored, stored, uplinkLinks(), obs); hasWarning(plan, "olr gateway add exit") {
		t.Error("a drift check repeated the setup note")
	}
}

// The case that motivated retiring: an uplink set on the wrong NIC and moved
// to the right one. The old address has to come off, and the plan has to say
// so on the interface it is actually on.
func TestMovingTheUplinkPlansTheOldAddressOff(t *testing.T) {
	links := StaticLinks{
		"ens18": {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")}},
		"ens19": {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.3/24")}},
	}
	before := Config{Uplink: &Uplink{Interface: "ens19", IPv4: &UplinkIPv4{
		Address: netip.MustParsePrefix("192.168.1.3/24"),
		Gateway: netip.MustParseAddr("192.168.1.1"),
	}}}
	after := before.Clone()
	after.Uplink.Interface = "ens18"
	after.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.1.2/24")
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")},
		Gateway:    netip.MustParseAddr("192.168.1.1"),
		GatewayDev: "ens19",
	}

	plan := buildPlan(before, after, links, obs)
	change, ok := changeAt(plan, "interfaces[ens19]")
	if !ok {
		t.Fatalf("no change on the old interface: %+v", plan.Changes)
	}
	if change.Diff != "- ip addr del 192.168.1.3/24 dev ens19\n" || change.Impact != impactDisruptive {
		t.Errorf("old interface change = %+v", change)
	}

	// Already gone: nothing to promise.
	delete(links, "ens19")
	links["ens19"] = LinkInfo{Adopted: true, Up: true}
	if _, ok := changeAt(buildPlan(before, after, links, obs), "interfaces[ens19]"); ok {
		t.Error("the plan promised to remove an address that is not there")
	}
}

// Renumbering in place retires the old address on the same interface — and
// the foreign-address note must not then call it somebody else's.
func TestRenumberingTheUplinkRetiresTheOldAddressInPlace(t *testing.T) {
	before := uplinkConfig()
	after := before.Clone()
	after.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.2.10/24")
	obs := Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
	plan := buildPlan(before, after, testLinks(), obs)
	if !slices.ContainsFunc(plan.Changes, func(c changeView) bool {
		return strings.Contains(c.Diff, "- ip addr del 192.168.2.9/24 dev enp2s0")
	}) {
		t.Errorf("the old address is not planned off: %+v", plan.Changes)
	}
	if hasWarning(plan, "also has") {
		t.Errorf("the retiring address was reported as foreign: %+v", plan.Warnings)
	}
}

// Somebody connected at the address being retired is told, by address, that
// their session ends with this change.
func TestRetiringTheCallersAddressWarnsByName(t *testing.T) {
	before := Config{Uplink: &Uplink{Interface: "ens19", IPv4: &UplinkIPv4{
		Address: netip.MustParsePrefix("192.168.1.3/24"),
		Gateway: netip.MustParseAddr("192.168.1.1"),
	}}}
	after := before.Clone()
	after.Uplink.Interface = "ens18"
	after.Uplink.IPv4.Address = netip.MustParsePrefix("192.168.1.2/24")

	r := httptest.NewRequest("POST", "/plan", nil)
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.ParseIP("192.168.1.3"), Port: 8080}))

	plan := withLockoutWarning(planView{}, r, before, after)
	if !hasWarning(plan, "192.168.1.3, which this takes off ens19") {
		t.Errorf("no warning naming the retired address: %+v", plan.Warnings)
	}
	if !hasWarning(plan, "Reconnect on 192.168.1.2") {
		t.Errorf("the warning does not say where to reconnect: %+v", plan.Warnings)
	}
}

func changeAt(plan planView, path string) (changeView, bool) {
	for _, c := range plan.Changes {
		if c.Path == path {
			return c, true
		}
	}
	return changeView{}, false
}
