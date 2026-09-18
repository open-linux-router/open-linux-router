package dns

import (
	"context"
	"os"
	"testing"
)

// What olrd asks this module at startup, and why it is not the drift question.
//
// The incident: the release that moved the resolver's config into /etc/unbound
// — where Debian's AppArmor profile lets the daemon read it — left every
// already-configured box with a unit pointing at a path nothing had written.
// The running unbound kept serving from the config it had already read, so
// nothing looked wrong until the next boot, when the config check failed on a
// file that was not there and the building lost DNS. Nothing re-rendered in
// between: the .deb's postinstall cannot, and `olr enable` is not run on an
// upgrade.
//
// So olrd re-renders at startup (internal/daemon's startDNS). What it must not
// do is re-render on the ordinary boot, where it is racing systemd for its own
// backends, and that is the whole reason the question it asks is RewritesFiles
// and not !Empty: a unit systemd has not started yet is drift and is none of
// olrd's business; a file that is not the one stored intent produces is both.

// The upgrade that moved the path, in the only form this package can see it:
// the rendered config is not there.
func TestAMissingRenderedConfigIsSomethingToRewrite(t *testing.T) {
	a, _, _ := testApplier(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, validConfig()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Exactly what an upgrade that moves UnboundConf leaves behind: intent in
	// the store, a unit expecting a file, and no file.
	if err := os.Remove(a.Paths.UnboundConf); err != nil {
		t.Fatalf("removing the rendered config: %v", err)
	}

	plan, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if !plan.RewritesFiles() {
		t.Errorf("a missing %s was not something to rewrite; startup would have left the box unable to resolve after its next reboot\nplan: %+v",
			a.Paths.UnboundConf, plan.Changes)
	}
}

// The ordinary boot. olrd comes up while systemd is still working through
// multi-user.target, so the backends it drives are legitimately not running
// yet. Answering "drifted" here would have olrd start a unit that is already
// starting, at every boot, forever.
func TestABackendSystemdHasNotStartedYetIsNotSomethingToRewrite(t *testing.T) {
	a, resolver, relay := testApplier(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, validConfig()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Every rendered file is on disk and correct; only the units are down.
	resolver.active = false
	relay.active = false

	plan, err := a.Drift(ctx)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}

	// The drift answer and the startup answer part company here, and that is
	// the point: `olr dns status` should still say the box is not what it says
	// it is, because an operator reading it wants to know the resolver is down.
	if plan.Empty() {
		t.Error("a stopped resolver was not reported as drift; `olr dns status` would call a box with no resolver correct")
	}
	if plan.RewritesFiles() {
		t.Errorf("a stopped resolver was treated as a file to rewrite; olrd would race systemd for its own backends at every boot\nplan: %+v",
			plan.Changes)
	}
}

// A box that has never been configured has no intent to restore, and startup
// must not invent any. The guard for this is in startDNS — it returns before
// planning when the module is switched off — but the shape it relies on is
// here: a disabled config plans services and touches no file.
func TestADisabledModulePlansNoFileToRewrite(t *testing.T) {
	a, _, _ := testApplier(t)
	ctx := context.Background()

	if _, err := a.Apply(ctx, validConfig()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	off := validConfig()
	off.Enabled = false
	off.Normalize()

	plan, err := a.Plan(ctx, off)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.RewritesFiles() {
		t.Errorf("switching the module off was read as a file to rewrite:\nplan: %+v", plan.Changes)
	}
}
