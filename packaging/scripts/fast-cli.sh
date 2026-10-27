#!/bin/sh
# Fetch unmodified upstream binaries and their license for redistribution.
set -eu
destination=${1:?usage: fast-cli.sh output-directory}
mkdir -p "$destination"
destination=$(cd "$destination" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
base=https://github.com/mikkelam/fast-cli/releases/download/v0.3.10
fetch() {
  name=$1
  digest=$2
  curl --fail --location --retry 3 "$base/$name" -o "$stage/$name"
  (cd "$stage" && printf '%s  %s\n' "$digest" "$name" | sha256sum -c -)
}
for arch in amd64 arm64; do
  case "$arch" in
    amd64) target=x86_64; digest=8b957bcb82f79337350e020a2b15f3b655fab0c71b7bfe23d09e2bd17eae1a77 ;;
    arm64) target=aarch64; digest=c45fb1be7986ac30247a43906c95d808614a48bb5489c01e155375ef63a38af0 ;;
  esac
  archive=fast-cli-$target-linux.tar.gz
  fetch "$archive" "$digest"
  mkdir "$stage/$arch"
  tar -xzf "$stage/$archive" -C "$stage/$arch"
  binary=$(find "$stage/$arch" -type f -name fast-cli)
  test -n "$binary"
  install -m 0755 "$binary" "$destination/fast-cli-$arch"
done
install -m 0644 "$stage/amd64/LICENSE" "$destination/fast-cli-LICENSE"
