#!/bin/sh
# Runs after the package's files are removed.
set -e

# On purge, and only on purge, take back the two files olr renders into another
# package's directories.
#
# /etc/open-linux-router is left in place even here, including olr.json and the
# rendered directory. Configuration is the operator's, not the package's: a
# removal that took the description of their network with it would make
# reinstalling a retyping exercise, and dpkg keeps conffiles for the same
# reason.
#
# These two are the exception, and the reason is whose directory they are in.
# The resolver's config and trust anchor are rendered under /etc/unbound and
# /var/lib/unbound because Debian's AppArmor profile confines /usr/sbin/unbound
# to those trees (internal/dns.Paths). They are generated rather than written by
# the operator, so there is nothing of theirs to keep — and leaving them would
# leave olr's litter in the unbound package's own directories, where a later
# `apt purge unbound` cannot clear it and the operator has no reason to look.
#
# Only our own subdirectory in each, never the tree: rm -rf on /etc/unbound
# would take the distribution's resolver with it. This runs before the systemd
# check below because it is true of a container image too, where there is no
# systemd to talk to and the files are on disk all the same.
if [ "$1" = purge ]; then
	rm -rf /etc/unbound/open-linux-router /var/lib/unbound/open-linux-router
fi

[ -d /run/systemd/system ] || exit 0

systemctl daemon-reload >/dev/null 2>&1 || true

exit 0
