#!/bin/sh
# Cross-compile cnw into bin/<os>-<arch>/ for every supported platform.
set -eu
cd "$(dirname "$0")/.."
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  out="bin/$os-$arch/cnw"
  [ "$os" = windows ] && out="$out.exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$out" ./cmd/cnw
  echo "built $out"
done
