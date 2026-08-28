#!/bin/sh
# Install olr from the tarball, for distributions the .deb does not cover.
#
# The .deb is the supported path: it declares the dnsmasq dependency, so apt
# resolves it before any of our code runs. Here that has to be checked by hand,
# which is the whole difference between the two.
set -e

PREFIX="${PREFIX:-/usr}"
UNITDIR="${UNITDIR:-/lib/systemd/system}"

[ "$(id -u)" = 0 ] || {
	echo "install.sh must run as root" >&2
	exit 1
}

cd "$(dirname "$0")"

# dnsmasq's location is not the same everywhere: Debian and Ubuntu put it in
# /usr/sbin, Arch and Alpine in /usr/bin. The shipped unit names Debian's path,
# so anything else gets a drop-in rather than an edited unit — an edited unit
# would be overwritten by the next upgrade.
DNSMASQ="$(command -v dnsmasq || true)"
if [ -z "$DNSMASQ" ]; then
	echo "dnsmasq was not found on PATH." >&2
	echo "olr does not implement DHCP itself; install your distribution's dnsmasq package first." >&2
	exit 1
fi

install -m 0755 -D olr "$PREFIX/bin/olr"
install -m 0755 -D olrd "$PREFIX/bin/olrd"
install -m 0644 -D systemd/olrd.service "$UNITDIR/olrd.service"
install -m 0644 -D systemd/olr-dhcp.service "$UNITDIR/olr-dhcp.service"
install -d -m 0755 /etc/open-linux-router

CONF=/etc/open-linux-router/rendered/dhcp/dnsmasq.conf
if [ "$DNSMASQ" != /usr/sbin/dnsmasq ]; then
	mkdir -p /etc/systemd/system/olr-dhcp.service.d
	cat >/etc/systemd/system/olr-dhcp.service.d/10-path.conf <<EOF
# Written by install.sh: dnsmasq is not at the Debian path this unit assumes.
[Service]
ExecStartPre=
ExecStartPre=$DNSMASQ --test -C $CONF
ExecStart=
ExecStart=$DNSMASQ -k -C $CONF
EOF
	echo "dnsmasq found at $DNSMASQ; wrote a drop-in overriding the unit's path."
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload
	systemctl enable --now olrd.service
	echo "olrd is running. olr-dhcp.service is intentionally left disabled;"
	echo "it is enabled when you configure DHCP (\`olr dhcp enable\`)."
else
	echo "No running systemd detected; skipped enabling olrd."
fi
