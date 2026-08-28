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
# olr-dhcp.service is deliberately NOT enabled. The dhcp module enables it when
# its configuration says DHCP is on; enabling it here would put a DHCP server on
# the network before anyone had configured one, which is precisely the surprise
# design.md §3.4 forbids.
systemctl enable olrd.service >/dev/null 2>&1 || true

if systemctl is-active --quiet olrd.service; then
	# An upgrade. Restarting is safe by design and worth stating: design.md
	# §3.5's governing invariant is that `systemctl restart olrd` never drops a
	# packet, expires a lease, or breaks a session — the backends are separate
	# units and keep serving throughout.
	systemctl restart olrd.service || true
else
	systemctl start olrd.service || true
fi

exit 0
