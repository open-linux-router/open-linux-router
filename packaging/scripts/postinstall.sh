#!/bin/sh
# Runs after the package is unpacked, on both a fresh install and an upgrade.
set -e

# Nothing to do in a chroot or a container image build: there is a systemd on
# disk but no systemd running to talk to. This is the standard test for it.
[ -d /run/systemd/system ] || exit 0

systemctl daemon-reload >/dev/null 2>&1 || true

# olrd is enabled and started here. Installing a router's control plane and
# leaving it stopped is a surprise, and every other surface — the CLI, the web
# UI — is a client of it, so a stopped olrd looks like a broken install.
#
# The backend units are deliberately NOT enabled. Each module enables its own
# when its configuration says that service is on; enabling them here would put a
# DHCP server on the network and take over :53 before anyone had configured
# either, which is precisely the surprise design.md §3.4 forbids. Taking over
# :53 uninvited would be the louder of the two — on most boxes something else
# already holds it, and olr would either fail to start or displace the resolver
# the machine was using.
systemctl enable olrd.service >/dev/null 2>&1 || true

FIRST_INSTALL=no
systemctl is-active --quiet olrd.service || FIRST_INSTALL=yes

if systemctl is-active --quiet olrd.service; then
	# An upgrade. Restarting is safe by design and worth stating: design.md
	# §3.5's governing invariant is that `systemctl restart olrd` never drops a
	# packet, expires a lease, or breaks a session — the backends are separate
	# units and keep serving throughout. That invariant is the whole reason the
	# DNS relay is its own binary: an olrd restart must not interrupt name
	# resolution for the building.
	systemctl restart olrd.service || true
else
	systemctl start olrd.service || true
fi

# Where to go next, printed once rather than left in a README nobody has yet.
#
# This used to print `sudo olr listen 0.0.0.0:8080` and point at a token file,
# because olrd served only its control socket and the UI was unreachable until
# somebody ran that command. Both are gone. olrd listens on :8080 from this
# moment (docs/system.md §1), and what makes that safe is not a credential but a
# capability: an unclaimed box refuses to be configured over the network until
# somebody opens the page and says how they want to reach it.
#
# The addresses come from `olr` rather than from a shell pipeline parsing ip(8).
# The binary already has to answer this for `olr enable`, and two
# implementations of "which URLs reach this box" would drift.
if [ "$FIRST_INSTALL" = yes ]; then
	cat <<'EOF'

olrd is running. Nothing else has been changed on this machine — no DHCP
server, no resolver, no firewall rule.

Open the web UI to finish setting up:

EOF
	olr internal urls 2>/dev/null || echo "  http://<this box>:8080"
	cat <<'EOF'

Or stay on the command line:

  sudo olr claim --no-password  set this box up
  olr link show interfaces      what this machine has
  sudo olr adopt <interface>    hand one to olr

EOF
fi

exit 0
