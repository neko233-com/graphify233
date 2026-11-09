#!/usr/bin/env bash
# Build, checksum, sign and (optionally) publish graphify233 releases.
#
#   ./release.sh                  # build v0.1.0 into dist/
#   ./release.sh v0.2.0 --publish # build and publish with gh
#
# Releases contain one static binary per platform plus checksums.txt and its
# Ed25519 signature. Private keys live in release/keys/ and are never committed.
set -euo pipefail

VERSION="v0.1.0"
PUBLISH=0
NO_SIGN=0
OUT_DIR="dist"
REPO="neko233-com/graphify233"

while [ $# -gt 0 ]; do
  case "$1" in
    --publish) PUBLISH=1 ;;
    --no-sign) NO_SIGN=1 ;;
    --out) shift; OUT_DIR="$1" ;;
    -h|--help)
      sed -n '2,12p' "$0"
      exit 0
      ;;
    -*) echo "unknown flag: $1" >&2; exit 1 ;;
    *) VERSION="$1" ;;
  esac
  shift
done

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

if [ -n "${GO:-}" ]; then
  GO_BIN="$GO"
elif command -v go >/dev/null 2>&1; then
  GO_BIN="$(command -v go)"
elif [ -x "$HOME/toolchains/go/bin/go" ]; then
  GO_BIN="$HOME/toolchains/go/bin/go"
else
  echo "Go toolchain not found (need 1.27+)." >&2
  exit 1
fi

KEY_DIR="release/keys"
KEY_PATH="$KEY_DIR/ed25519.key"
PUB_PATH="$KEY_DIR/ed25519.pub"
if [ ! -f "$KEY_PATH" ]; then
  echo "no signing key found; generating $KEY_PATH"
  "$GO_BIN" run ./cmd/gf-release keygen -key "$KEY_PATH"
  echo "commit $PUB_PATH and keep $KEY_PATH secret."
fi
PUB="$(tr -d '\r\n' < "$PUB_PATH")"
echo "signing key: $PUB"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

LDFLAGS="-s -w -X github.com/$REPO/internal/selfupdate.Version=$VERSION -X github.com/$REPO/internal/selfupdate.PublicKeyB64=$PUB"

build() {
  os="$1"; arch="$2"; ext="$3"
  name="graphify233_${os}_${arch}${ext}"
  echo "building $name"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 "$GO_BIN" build -trimpath -buildvcs=false \
    -ldflags "$LDFLAGS" -o "$OUT_DIR/$name" ./cmd/graphify233
}

build windows amd64 .exe
build windows arm64 .exe
build linux   amd64 ""
build linux   arm64 ""
build darwin  amd64 ""
build darwin  arm64 ""

"$GO_BIN" run ./cmd/gf-release checksums -dir "$OUT_DIR"

if [ "$NO_SIGN" -eq 0 ]; then
  "$GO_BIN" run ./cmd/gf-release sign -key "$KEY_PATH" \
    -in "$OUT_DIR/checksums.txt" -out "$OUT_DIR/checksums.txt.sig"
fi

echo
echo "release artifacts in $OUT_DIR:"
ls -l "$OUT_DIR"

if [ "$PUBLISH" -eq 1 ]; then
  command -v gh >/dev/null 2>&1 || { echo "gh CLI required for --publish" >&2; exit 1; }
  gh release create "$VERSION" "$OUT_DIR"/* --repo "$REPO" \
    --title "graphify233 $VERSION" \
    --notes "graphify233 $VERSION - single static binary, zero dependencies."
  echo "published $VERSION"
fi
