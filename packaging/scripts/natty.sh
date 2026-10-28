#!/bin/sh
# Fetch the unmodified upstream NatTypeTester console binaries and its license.
set -eu
destination=${1:?usage: natty.sh output-directory}
mkdir -p "$destination"
destination=$(cd "$destination" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
version=10.0.10
base=https://github.com/HMBSbige/NatTypeTester/releases/download/$version
command -v 7z >/dev/null 2>&1 || {
  echo "7z not found. Install p7zip-full or 7-zip to extract NatTypeTester." >&2
  exit 1
}
fetch() {
  name=$1
  digest=$2
  curl --fail --location --retry 3 "$base/$name" -o "$stage/$name"
  (cd "$stage" && printf '%s  %s\n' "$digest" "$name" | sha256sum -c -)
}
for arch in amd64 arm64; do
  case "$arch" in
    amd64) target=linux-x64; digest=2a8346c806d016a02975eb2fa959a511fb989c17f10dfb09ede2de8db635b8b3 ;;
    arm64) target=linux-arm64; digest=b758a72510d0b6e5ee26a512db7a7b9a6f54d44468df899e0f1841ad54d30d85 ;;
  esac
  archive=NatTypeTester.Console-$version-$target.7z
  fetch "$archive" "$digest"
  mkdir "$stage/$arch"
  7z x -y -o"$stage/$arch" "$stage/$archive" >/dev/null
  binary=$(find "$stage/$arch" -type f -name NatTypeTester.Console)
  test -n "$binary"
  install -m 0755 "$binary" "$destination/natty-$arch"
done
curl --fail --location --retry 3 "https://raw.githubusercontent.com/HMBSbige/NatTypeTester/$version/LICENSE" -o "$stage/LICENSE"
printf '%s  %s\n' "ff4cd587c6641f87dadbe8d2910e7b4c1547ed72e2920728ec27faf8009920ae" "LICENSE" | (cd "$stage" && sha256sum -c -)
install -m 0644 "$stage/LICENSE" "$destination/NatTypeTester-LICENSE"
