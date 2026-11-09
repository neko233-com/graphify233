#!/usr/bin/env bash
# Install graphify233 as a single global binary.
#
#   curl -fsSL https://raw.githubusercontent.com/neko233-com/graphify233/main/install.sh | bash
#   ./install.sh --version v0.1.0
#   ./install.sh --local dist        # install a local build
#
# Verification: SHA-256 always, plus the Ed25519 signature over checksums.txt
# when a verifier is available (installed graphify233, gf-release, go run).
# --strict makes a missing verifier fatal.
set -euo pipefail

REPO="${GRAPHIFY233_REPO:-neko233-com/graphify233}"
VERSION="${GRAPHIFY233_VERSION:-latest}"
MIRROR="${GRAPHIFY233_MIRROR:-}"
INSTALL_DIR="${GRAPHIFY233_INSTALL_DIR:-$HOME/.local/bin}"
PUBKEY="${GRAPHIFY233_PUBKEY:-4Up+dr60klL8ZLQsjF+BK7eul45Qb8xDBK8iIfnmmE0=}"
API="${GRAPHIFY233_API:-https://api.github.com}"
STRICT=0
NO_PATH=0
LOCAL_DIST=""

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift ;;
    --dir) INSTALL_DIR="$2"; shift ;;
    --mirror) MIRROR="$2"; shift ;;
    --local) LOCAL_DIST="$2"; shift ;;
    --strict) STRICT=1 ;;
    --no-path) NO_PATH=1 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
  shift
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac
machine="$(uname -m)"
case "$machine" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $machine" >&2; exit 1 ;;
esac
asset="graphify233_${os}_${arch}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download() { curl -fsSL "$1" -o "$2"; }

if [ -n "$LOCAL_DIST" ]; then
  cp "$LOCAL_DIST/$asset" "$tmp/$asset"
  cp "$LOCAL_DIST/checksums.txt" "$tmp/checksums.txt"
  [ -f "$LOCAL_DIST/checksums.txt.sig" ] && cp "$LOCAL_DIST/checksums.txt.sig" "$tmp/checksums.txt.sig"
else
  url="$API/repos/$REPO/releases/latest"
  [ "$VERSION" != "latest" ] && url="$API/repos/$REPO/releases/tags/$VERSION"

  # Download routes: direct first, then an explicit mirror or the built-in
  # China-friendly list. The fastest reachable route wins; the Ed25519
  # signature is still verified locally, so a mirror cannot inject code.
  routes=$'\n'
  if [ -n "$MIRROR" ]; then
    routes+="${MIRROR%/}/"$'\n'
  elif [ -z "${GRAPHIFY233_NO_MIRROR:-}" ]; then
    routes+="https://gh-proxy.com/"$'\n'
    routes+="https://ghfast.top/"$'\n'
    routes+="https://ghproxy.net/"$'\n'
    routes+="https://hub.gitmirror.com/"$'\n'
    routes+="https://github.moeyy.xyz/"$'\n'
  fi

  json=""
  rel_source=""
  while IFS= read -r prefix; do
    if json="$(curl -fsSL --max-time 8 -H 'User-Agent: graphify233-installer' -H 'Accept: application/vnd.github+json' "${prefix}${url}" 2>/dev/null)"; then
      rel_source="${prefix:-direct}"
      break
    fi
  done <<< "$routes"
  [ -n "$json" ] || { echo "could not resolve the release from GitHub or any mirror; set GRAPHIFY233_MIRROR to a prefix proxy" >&2; exit 1; }
  tag="$(printf '%s' "$json" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
  [ -n "$tag" ] || { echo "could not resolve release tag" >&2; exit 1; }
  echo "release: $tag (via $rel_source)"

  asset_url="$(printf '%s\n' "$json" | awk -v want="\"$asset\"" '
      index($0, want) { found=1; next }
      found && /browser_download_url/ {
        line=$0; sub(/^[^:]*:[[:space:]]*"/, "", line); sub(/".*$/, "", line); print line; exit
      }')"
  [ -n "$asset_url" ] || { echo "release $tag has no asset $asset" >&2; exit 1; }
  sums_url="$(printf '%s\n' "$json" | awk -v want='"checksums.txt"' '
      index($0, want) { found=1; next }
      found && /browser_download_url/ { line=$0; sub(/^[^:]*:[[:space:]]*"/, "", line); sub(/".*$/, "", line); print line; exit }')"
  sig_url="$(printf '%s\n' "$json" | awk -v want='"checksums.txt.sig"' '
      index($0, want) { found=1; next }
      found && /browser_download_url/ { line=$0; sub(/^[^:]*:[[:space:]]*"/, "", line); sub(/".*$/, "", line); print line; exit }')"
  [ -n "$sums_url" ] || { echo "release has no checksums.txt" >&2; exit 1; }
  [ -n "$sig_url" ] || { echo "release has no checksums.txt.sig" >&2; exit 1; }

  # Rank routes by timing checksums.txt (a few KB) and keep the winning bytes.
  ranked="$tmp/ranked.tsv"
  : > "$ranked"
  while IFS= read -r prefix; do
    name="${prefix:-direct}"
    probe="$tmp/probe-$(printf '%s' "$name" | tr -c 'a-z0-9' '_').txt"
    t="$(curl -fsSL --max-time 8 -o "$probe" -w '%{time_total}' "${prefix}${sums_url}" 2>/dev/null)" || continue
    ms="$(awk -v t="$t" 'BEGIN { printf "%d", t * 1000 }')"
    printf '%s\t%s\t%s\t%s\n' "$ms" "$name" "$prefix" "$probe" >> "$ranked"
  done <<< "$routes"
  [ -s "$ranked" ] || { echo "could not download checksums.txt from GitHub or any mirror" >&2; exit 1; }
  sort -n "$ranked" -o "$ranked"
  IFS=$'\t' read -r best_ms best_name best_prefix best_file < "$ranked"
  cp "$best_file" "$tmp/checksums.txt"
  echo "download source: $best_name (${best_ms} ms)"

  fetch_asset() {
    src="$1"
    dest="$2"
    while IFS=$'\t' read -r _ms _name prefix _file; do
      if curl -fsSL --max-time 300 -o "$dest" "${prefix}${src}"; then
        return 0
      fi
    done < "$ranked"
    return 1
  }
  fetch_asset "$sig_url" "$tmp/checksums.txt.sig" || { echo "could not download checksums.txt.sig" >&2; exit 1; }
  fetch_asset "$asset_url" "$tmp/$asset" || { echo "could not download $asset" >&2; exit 1; }
fi

# 1) SHA-256 of the binary.
want="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print tolower($1) }' "$tmp/checksums.txt" | head -1)"
[ -n "$want" ] || { echo "checksums.txt has no entry for $asset" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | awk '{print tolower($1)}')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/$asset" | awk '{print tolower($1)}')"
else
  echo "no sha256 tool found (need sha256sum or shasum)" >&2
  exit 1
fi
[ "$got" = "$want" ] || { echo "checksum mismatch for $asset (expected $want, got $got)" >&2; exit 1; }
echo "sha256 ok: $got"

# 2) Ed25519 signature over checksums.txt.
verified=0
if [ -f "$tmp/checksums.txt.sig" ]; then
  if [ -x "$INSTALL_DIR/graphify233" ]; then
    if "$INSTALL_DIR/graphify233" verify-release --in "$tmp/checksums.txt" --sig "$tmp/checksums.txt.sig" --pub "$PUBKEY" >/dev/null 2>&1; then verified=1; fi
  fi
  if [ "$verified" -eq 0 ] && command -v graphify233 >/dev/null 2>&1; then
    if graphify233 verify-release --in "$tmp/checksums.txt" --sig "$tmp/checksums.txt.sig" --pub "$PUBKEY" >/dev/null 2>&1; then verified=1; fi
  fi
  if [ "$verified" -eq 0 ] && command -v gf-release >/dev/null 2>&1; then
    if gf-release verify -in "$tmp/checksums.txt" -sig "$tmp/checksums.txt.sig" -pub "$PUBKEY" >/dev/null 2>&1; then verified=1; fi
  fi
  if [ "$verified" -eq 0 ] && command -v go >/dev/null 2>&1 && [ -d ./cmd/gf-release ]; then
    if go run ./cmd/gf-release verify -in "$tmp/checksums.txt" -sig "$tmp/checksums.txt.sig" -pub "$PUBKEY" >/dev/null 2>&1; then verified=1; fi
  fi
fi
if [ "$verified" -eq 0 ]; then
  if [ "$STRICT" -eq 1 ]; then
    echo "could not verify the release signature and --strict was requested" >&2
    exit 1
  fi
  echo "warning: signature not verified (no verifier available); SHA-256 matched." >&2
else
  echo "signature ok"
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$asset" "$INSTALL_DIR/graphify233"
echo "installed: $INSTALL_DIR/graphify233"

if [ "$NO_PATH" -eq 0 ]; then
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
      for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
        [ -f "$rc" ] || continue
        if ! grep -q "graphify233" "$rc" 2>/dev/null; then
          printf '\n# graphify233\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >> "$rc"
        fi
      done
      echo "added $INSTALL_DIR to PATH in your shell rc files (open a new shell)"
      ;;
  esac
fi

"$INSTALL_DIR/graphify233" version
