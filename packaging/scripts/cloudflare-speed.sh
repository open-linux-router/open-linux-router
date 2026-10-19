#!/bin/sh
# Fetch unmodified upstream binaries and corresponding source for redistribution.
set -eu
destination=${1:?usage: cloudflare-speed.sh output-directory}
mkdir -p "$destination"
destination=$(cd "$destination" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
base=https://github.com/kavehtehrani/cloudflare-speed-cli/releases/download/v1.0.10
fetch() {
  name=$1
  digest=$2
  curl --fail --location --retry 3 "$base/$name" -o "$stage/$name"
  (cd "$stage" && printf '%s  %s\n' "$digest" "$name" | sha256sum -c -)
}
for arch in amd64 arm64; do
  case "$arch" in
    amd64) target=x86_64; digest=4e2511592f6568382f90d9813401c32e28b407b3d644866736ae2318c0810da1 ;;
    arm64) target=aarch64; digest=ed82e6f8a769e30e82cb1aa07da746ce062394a5eb45e4e27c074c5e39f74e23 ;;
  esac
  archive=cloudflare-speed-cli-$target-unknown-linux-musl.tar.xz
  fetch "$archive" "$digest"
  mkdir "$stage/$arch"
  tar -xJf "$stage/$archive" -C "$stage/$arch"
  binary=$(find "$stage/$arch" -type f -name cloudflare-speed-cli)
  test -n "$binary"
  install -m 0755 "$binary" "$destination/cloudflare-speed-cli-$arch"
done
fetch source.tar.gz 4d13e067e9dd45b89d804390feafaa5ca37e38b40a9cad91af98401dab6f4018
cp "$stage/source.tar.gz" "$destination/cloudflare-speed-cli-source.tar.gz"
tar -xOf "$stage/source.tar.gz" cloudflare-speed-cli-1.0.10/LICENSE > "$destination/cloudflare-speed-cli-LICENSE"
