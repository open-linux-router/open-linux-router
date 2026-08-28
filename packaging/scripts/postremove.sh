#!/bin/sh
# Runs after the package's files are removed.
set -e

[ -d /run/systemd/system ] || exit 0

systemctl daemon-reload >/dev/null 2>&1 || true

# /etc/open-linux-router is left in place, including olr.json and the rendered
# directory. Configuration is the operator's, not the package's: a removal that
# took the description of their network with it would make reinstalling a
# retyping exercise, and dpkg keeps conffiles for the same reason.

exit 0
