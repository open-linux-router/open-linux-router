#!/bin/sh
# Runs before the package's files are removed.
set -e

# dpkg calls this for an upgrade too, as `prerm upgrade <version>`. Stopping the
# DHCP server on every upgrade would drop the thing the upgrade is supposed to
# preserve, so act only on an actual removal and treat anything unrecognised as
# "not a removal".
case "$1" in
remove | purge | 0) ;;
*) exit 0 ;;
esac

[ -d /run/systemd/system ] || exit 0

# olr-dhcp goes down with olr. The alternative is a dnsmasq left serving a
# network from rendered files that nothing maintains any more, which would
# outlive the tool that is supposed to own it.
for unit in olr-dhcp.service olrd.service; do
	systemctl stop "$unit" >/dev/null 2>&1 || true
	systemctl disable "$unit" >/dev/null 2>&1 || true
done

exit 0
