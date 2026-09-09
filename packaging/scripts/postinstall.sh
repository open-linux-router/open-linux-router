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

# What to do next, printed once rather than left in a README nobody has yet.
#
# The web UI is the reason this is here. olrd listens on its control socket and
# nothing else, so a fresh install has a working `olr` and a UI that cannot be
# reached from anywhere — which looks like a broken package rather than the
# deliberate choice it is (design.md §7: install alone changes nothing). Saying
# so, with the one command that changes it, is the difference.
if [ "$FIRST_INSTALL" = yes ]; then
	cat <<'EOF'

olrd is running. Nothing else has been changed on this machine — no DHCP
server, no resolver, no firewall rule.

To open the web UI on your network:

  sudo olr listen 0.0.0.0:8080

Then browse to http://<this box>:8080 and paste the token from
/etc/open-linux-router/api-token when asked.

Or stay on the command line:

  olr link show interfaces      what this machine has
  sudo olr adopt <interface>    hand one to olr
  olr dhcp --help               then serve addresses on it

EOF
fi

exit 0
