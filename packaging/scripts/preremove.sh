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

# The backends go down with olr. The alternative is a dnsmasq or an unbound left
# serving a network from rendered files that nothing maintains any more, which
# would outlive the tool that is supposed to own it.
#
# olr-dnsd first, and the order matters here in a way it does not for the
# others: stopping it runs the ExecStopPost that removes the nftables redirect.
# Leaving that table behind would send every device's DNS to a port nothing is
# listening on — a box with olr uninstalled and no working name resolution,
# which is a far worse parting gift than a stopped service.
for unit in olr-dnsd.service olr-dns.service olr-dhcp.service olrd.service; do
	systemctl stop "$unit" >/dev/null 2>&1 || true
	systemctl disable "$unit" >/dev/null 2>&1 || true
done

exit 0
