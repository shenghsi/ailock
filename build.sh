#!/usr/bin/env bash
# Cross-compile ailock for every supported platform into dist/.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist

# target goos goarch [extra GOENV=VALUE ...]
targets=(
  "linux   amd64"
  "linux   arm64"
  "linux   arm    GOARM=5"
  "linux   mips   GOMIPS=softfloat"
  "linux   mipsle GOMIPS=softfloat"
  "windows amd64"
  "windows arm64"
  "darwin  amd64"
  "darwin  arm64"
)

for t in "${targets[@]}"; do
  read -r goos goarch extra <<<"$t"
  name="ailock-${goos}-${goarch}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  # shellcheck disable=SC2086
  env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" $extra \
    go build -trimpath -ldflags "-s -w" -o "dist/${name}${ext}" .
  echo "built dist/${name}${ext}"
done
