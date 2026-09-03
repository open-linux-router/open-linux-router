#!/bin/sh
# Install olr from the tarball, for distributions the .deb does not cover.
#
# The .deb is the supported path: it declares the dnsmasq, unbound and nftables
# dependencies, so apt resolves them before any of our code runs. Here that has
# to be checked by hand, which is the whole difference between the two.
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

# unbound is the same story. It is required rather than optional for the same
# reason dnsmasq is: olr does not resolve names itself and never will
# (docs/dns.md §6 — recursion and DNSSEC validation are permanently out of
# scope), so without it the dns module has nothing to forward to.
UNBOUND="$(command -v unbound || true)"
UNBOUND_CHECKCONF="$(command -v unbound-checkconf || true)"
UNBOUND_ANCHOR="$(command -v unbound-anchor || true)"
if [ -z "$UNBOUND" ] || [ -z "$UNBOUND_CHECKCONF" ]; then
	echo "unbound was not found on PATH." >&2
	echo "olr does not resolve names itself; install your distribution's unbound package first." >&2
	exit 1
fi

# nftables is needed only for the DNS redirect, so a missing one is a warning
# rather than a refusal: everything else works, and the operator finds out now
# rather than when they turn the redirect on.
NFT="$(command -v nft || true)"
if [ -z "$NFT" ]; then
	echo "warning: nft was not found on PATH." >&2
	echo "Everything works except the DNS redirect (\`olr dns set --redirect\`)," >&2
	echo "which needs your distribution's nftables package." >&2
fi

install -m 0755 -D olr "$PREFIX/bin/olr"
install -m 0755 -D olrd "$PREFIX/bin/olrd"
install -m 0755 -D olr-dnsd "$PREFIX/bin/olr-dnsd"
install -m 0644 -D systemd/olrd.service "$UNITDIR/olrd.service"
install -m 0644 -D systemd/olr-dhcp.service "$UNITDIR/olr-dhcp.service"
install -m 0644 -D systemd/olr-dns.service "$UNITDIR/olr-dns.service"
install -m 0644 -D systemd/olr-dnsd.service "$UNITDIR/olr-dnsd.service"
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

UNBOUND_CONF=/etc/open-linux-router/rendered/dns/unbound.conf
ANCHOR=/var/lib/open-linux-router/dns/root.key
if [ "$UNBOUND" != /usr/sbin/unbound ]; then
	mkdir -p /etc/systemd/system/olr-dns.service.d
	# Both ExecStartPre lines are cleared and rewritten, because clearing the
	# directive resets the whole list rather than one entry — reinstating only
	# the second would silently drop the trust-anchor bootstrap.
	{
		echo "# Written by install.sh: unbound is not at the Debian path this unit assumes."
		echo "[Service]"
		echo "ExecStartPre="
		if [ -n "$UNBOUND_ANCHOR" ]; then
			echo "ExecStartPre=-$UNBOUND_ANCHOR -a $ANCHOR"
		fi
		echo "ExecStartPre=$UNBOUND_CHECKCONF $UNBOUND_CONF"
		echo "ExecStart="
		echo "ExecStart=$UNBOUND -d -p -c $UNBOUND_CONF"
	} >/etc/systemd/system/olr-dns.service.d/10-path.conf
	echo "unbound found at $UNBOUND; wrote a drop-in overriding the unit's path."
fi

if [ -n "$NFT" ] && [ "$NFT" != /usr/sbin/nft ]; then
	mkdir -p /etc/systemd/system/olr-dnsd.service.d
	cat >/etc/systemd/system/olr-dnsd.service.d/10-path.conf <<EOF
# Written by install.sh: nft is not at the Debian path this unit assumes.
[Service]
ExecStartPost=
ExecStartPost=+-/bin/sh -c 'f=/etc/open-linux-router/rendered/dns/hijack.nft; test -f "\$f" || exit 0; exec $NFT -f "\$f"'
ExecStopPost=
ExecStopPost=+-$NFT delete table inet olr-dns
EOF
	echo "nft found at $NFT; wrote a drop-in overriding the unit's path."
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload
	systemctl enable --now olrd.service
	echo "olrd is running. The backend units are intentionally left disabled;"
	echo "each is enabled when you configure its module (\`olr dhcp enable\`,"
	echo "\`olr dns enable\`)."
else
	echo "No running systemd detected; skipped enabling olrd."
fi
