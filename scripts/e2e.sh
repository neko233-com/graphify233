#!/usr/bin/env bash
# End-to-end check: build -> query -> analyze -> diff -> serve-http -> MCP.
# Runs against a throwaway corpus; safe in CI and locally.
set -euo pipefail

GO="${GO:-go}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
BIN="$TMP/graphify233"
CORPUS="$TMP/corpus"
PORT="${E2E_PORT:-7399}"
TOKEN="e2e-token"
SERVER_PID=""

cleanup() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok: $*"; }

echo "== build binary"
(cd "$ROOT" && "$GO" build -o "$BIN" ./cmd/graphify233)

echo "== corpus"
mkdir -p "$CORPUS"

cat > "$CORPUS/app.py" <<'PY'
import os
from db import connect

class Loader:
    def load_user(self, uid):
        return connect(uid)

def main():
    loader = Loader()
    return loader.load_user(1)
PY

cat > "$CORPUS/schema.sql" <<'SQL'
CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);
CREATE TABLE orders (
  id INTEGER PRIMARY KEY,
  user_id INTEGER REFERENCES users(id)
);
CREATE VIEW order_summary AS SELECT o.id, u.email FROM orders o JOIN users u ON u.id = o.user_id;
SQL

cat > "$CORPUS/main.tf" <<'TF'
resource "aws_s3_bucket" "assets" {
  bucket = "assets"
}
resource "aws_cloudfront_distribution" "cdn" {
  origin = aws_s3_bucket.assets.id
}
TF

cat > "$CORPUS/Dockerfile" <<'DOCKER'
FROM golang:1.27 AS build
RUN go build -o /app .
FROM alpine AS runtime
COPY --from=build /app /app
DOCKER

cat > "$CORPUS/openapi.yaml" <<'YAML'
openapi: 3.0.0
info:
  title: Orders API
paths:
  /orders:
    get:
      summary: list orders
    post:
      summary: create order
YAML

cat > "$CORPUS/guide.md" <<'MD'
# Loading users

The `load_user` helper validates input first.

## Database

`connect` opens the pool; `users` and `orders` live in schema.sql.
MD

echo "== build"
"$BIN" build "$CORPUS" --mode deep --semantic --quiet
[ -f "$CORPUS/graphify-out/graph.json" ] || fail "graph.json missing"
pass "graph.json written"

echo "== extractors and mentions"
grep -q '"relation": "mentions"' "$CORPUS/graphify-out/graph.json" || fail "no mentions edges from docs"
grep -q '"label": "main.tf"' "$CORPUS/graphify-out/graph.json" || fail "terraform extractor missing"
pass "deep links and new extractors present"

echo "== query --json"
Q="$("$BIN" query load_user --graph "$CORPUS/graphify-out/graph.json" --json)"
echo "$Q" | grep -q '"question": "load_user"' || fail "query json shape"
echo "$Q" | grep -q '"nodes"' || fail "query returned no nodes"
pass "query returns a subgraph"

echo "== analyze"
OUT="$("$BIN" analyze --graph "$CORPUS/graphify-out/graph.json")"
echo "$OUT" | grep -q '^graph:' || fail "analyze output"
pass "analyze runs"

echo "== diff (identical snapshot)"
"$BIN" diff "$CORPUS/graphify-out/graph.json" "$CORPUS/graphify-out/graph.json" | grep -q "no structural changes" \
  || fail "self diff reported changes"
pass "self diff is empty"

echo "== serve-http"
"$BIN" serve-http --graph "$CORPUS/graphify-out/graph.json" --addr "127.0.0.1:$PORT" --token "$TOKEN" >"$TMP/http.log" 2>&1 &
SERVER_PID=$!
ready=0
for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then ready=1; break; fi
  sleep 0.2
done
[ "$ready" = "1" ] || { cat "$TMP/http.log"; fail "http server did not start"; }
pass "healthz"

code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/stats")"
[ "$code" = "401" ] || fail "unauthenticated /stats returned $code"
code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/stats")"
[ "$code" = "200" ] || fail "authorized /stats returned $code"
pass "bearer auth enforced"

printf '%s' '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"graph_stats","arguments":{}}}' > "$TMP/mcp.json"
MCP="$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data-binary "@$TMP/mcp.json" "http://127.0.0.1:$PORT/mcp")"
echo "$MCP" | grep -q '"result"' || fail "mcp response: $MCP"
pass "mcp graph_stats"

kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""

echo "== watch debounce (optional)"
if [ "${E2E_WATCH:-0}" = "1" ]; then
  "$BIN" watch "$CORPUS" --interval 1s --debounce 200ms >"$TMP/watch.log" 2>&1 &
  SERVER_PID=$!
  sleep 2
  echo "# edit" >> "$CORPUS/guide.md"
  sleep 0.2
  echo "# edit two" >> "$CORPUS/guide.md"
  sleep 5
  kill "$SERVER_PID" 2>/dev/null || true
  SERVER_PID=""
  [ "$(grep -c 'change detected' "$TMP/watch.log")" = "1" ] || { cat "$TMP/watch.log"; fail "watch should coalesce edits"; }
  pass "watch coalesced two writes"
fi

echo "PASS: end-to-end chain verified"
