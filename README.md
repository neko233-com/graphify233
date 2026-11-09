# graphify233

A knowledge graph for a codebase, written in Go. Point it at a directory and it
extracts symbols and relationships from 13 languages plus Markdown, PDFs and
images, clusters them into communities, and writes a queryable graph — all
locally, deterministically, with no API keys and no runtime dependencies.

This is a ground-up Go rewrite of [Graphify-Labs/graphify](https://github.com/Graphify-Labs/graphify)
built around one goal: **speed**. No tree-sitter, no cgo, no Python. A
hand-written lexer and per-language structural scanners parse the corpus with
parallel workers and an on-disk cache.

```
$ graphify233 build .
19 files · 23,229 words · 148 nodes · 223 edges · 15 communities · 33ms
```

## Install

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/neko233-com/graphify233/main/install.ps1 | iex
```

Linux / macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/neko233-com/graphify233/main/install.sh | bash
```

Both installers verify the SHA-256 of the downloaded binary and, when a
verifier is available, the Ed25519 signature over `checksums.txt`. Use
`-Strict` / `--strict` to make a missing verifier fatal. A single binary is
installed globally (Windows: `%LOCALAPPDATA%\Programs\graphify233\bin`;
Unix: `~/.local/bin`); each project keeps only its own `graphify-out/` cache.

From source:

```sh
go build -o graphify233 ./cmd/graphify233
```

### Mirrors (restricted networks)

`install.ps1`, `install.sh` and `graphify233 self-update` try GitHub directly
and through a built-in list of prefix mirrors (`gh-proxy.com`, `ghfast.top`,
`ghproxy.net`, `hub.gitmirror.com`, `github.moeyy.xyz`). They probe the routes,
pick the fastest reachable one for the release lookup and the asset downloads,
and fall back down the ranking when one route fails. The Ed25519 signature is
always verified locally, so a mirror cannot inject a binary or a forged
checksum file.

```powershell
$env:GRAPHIFY233_MIRROR = "https://ghfast.top"   # pin one mirror (prefix style)
$env:GRAPHIFY233_NO_MIRROR = "1"                 # disable automatic mirrors
$env:GRAPHIFY233_API = "https://api.kkgithub.com" # override the release API base
```

```sh
GRAPHIFY233_MIRROR=https://ghfast.top \
  curl -fsSL https://raw.githubusercontent.com/neko233-com/graphify233/main/install.sh | bash
```

`GRAPHIFY233_MIRROR` disables the built-in list so the pinned mirror is tried
first. `GRAPHIFY233_NO_MIRROR` keeps direct GitHub access only.

Release assets normally come from `github.com/.../releases/download/...`. If that
CDN answers with a transient error, the updater retries the same bytes through the
GitHub release-asset API; the signature and SHA-256 checks are unchanged.

## Quick start
```sh
graphify233 build .              # build graphify-out/ for the current project
graphify233 query "how does auth work"
graphify233 path detect extract  # shortest path between two symbols
graphify233 explain extract      # symbol detail and connections
graphify233 diff old.json graph.json --impact
graphify233 analyze              # cycles, unreferenced symbols, hubs
graphify233 doctor               # environment and graph health check
graphify233 serve-http           # JSON API + remote MCP on 127.0.0.1:7331
graphify233 benchmark            # token-savings report
```

`build` writes to `graphify-out/`:

| File | Contents |
| --- | --- |
| `graph.json` | node-link graph (nodes, links, hyperedges) |
| `GRAPH_REPORT.md` | corpus check, god nodes, surprises, communities, gaps, questions |
| `graph.html` | self-contained interactive viewer (canvas, no CDN) |
| `obsidian/` | one note per node, with wikilinks |
| `wiki/` | community/god-node articles (`--wiki`) |
| `graph.svg`, `graph.graphml`, `graph.cypher`, `graph.canvas` | optional exports |
| `manifest.json`, `cache/` | incremental rebuild state |

## Commands

```
graphify233 build [dir] [flags]     build or update the graph
graphify233 update [dir]            incremental rebuild (changed files only)
graphify233 scan [dir]              corpus stats without building
graphify233 query "question"        ranked subgraph as text
graphify233 query "..." --json      machine-readable subgraph (nodes, edges, tokens)
graphify233 explain NAME            symbol detail
graphify233 path FROM TO            shortest path
graphify233 diff OLD.json NEW.json  added/removed nodes and edges between snapshots
graphify233 diff OLD NEW --impact   ... plus reverse dependencies of each change
graphify233 analyze [dir]           cycles, unreferenced symbols, god nodes (--json)
graphify233 doctor [dir]            graph/manifest/cache/update/token health check
graphify233 benchmark [dir]         token reduction report
graphify233 mcp [dir]               MCP server over stdio
graphify233 serve-http [dir]        HTTP JSON API + streamable MCP at /mcp
graphify233 watch [dir]             rebuild on file changes (--debounce 500ms)
graphify233 add URL [dir]           ingest a web page into the graph
graphify233 hook install|status|uninstall
graphify233 install claude [dir]    Claude Code skill + slash command + project guidance
graphify233 install vscode [dir]    add a VS Code build task
graphify233 install uninstall [dir] remove all of the above
graphify233 cache clear|info
graphify233 self-update             verified self-update
graphify233 verify-release          verify a checksums manifest
```

Build flags: `--mode standard|deep`, `--update`, `--workers N`, `--out DIR`,
`--wiki --svg --graphml --cypher --canvas`, `--no-json --no-report --no-html
--no-obsidian`, `--neo4j URL --neo4j-user --neo4j-pass`, `--quiet`,
`--semantic [--semantic-local] [--semantic-url URL --semantic-model NAME
--semantic-key KEY --semantic-threshold 0.82]`.

`--mode deep` never calls an LLM: extraction stays deterministic, and the flag
widens the analysis (more god nodes and surprising connections) and links
documentation to code. In deep mode every Markdown/text/PDF file is scanned for
identifiers that match a uniquely named code symbol; each hit becomes an
INFERRED `mentions` edge with a source line, so questions like "how does
authentication work" can start from the docs. Markdown headings also become
document concept nodes and link to the symbols mentioned in their section
(`concept→symbol mentions`). `graphify233 query` tries code symbols first, then
document concepts, then falls back to mentions.

Optional semantic layer: `--semantic` adds `semantically_similar_to` edges
between concept sections with overlapping (IDF-weighted) symbol sets.
`--semantic-local` additionally embeds symbol labels with a deterministic
offline hashing embedder (no network, no key), and `--semantic-url` uses any
OpenAI-compatible `/embeddings` endpoint instead, connecting nearest neighbours
above `--semantic-threshold`. The local embedder skips identically named symbols
and requires a shared token, so generic names (`get`, `process`) do not form
cliques from hash collisions. All layers are opt-in; the default build stays
local and network-free.

Communities are numbered by an anchor node (highest degree, smallest id on
ties) rather than by size, so a rebuild of an unchanged tree reproduces the
same ids and `graphify233 diff` reports only real drift.

`graphify233 install claude` writes the Claude Code skill and slash command into
`~/.claude/`, and adds a short `<!-- graphify233:start -->` section to the
project's `CLAUDE.md` and `.github/copilot-instructions.md`. Existing content is
preserved, re-running refreshes only that section, and `install uninstall`
removes it again. `install vscode` merges a build task into `.vscode/tasks.json`.

## GitHub Actions

`action.yml` is a composite action: it installs the release binary, builds the
graph, and uploads `graphify-out/` as an artifact.

```yaml
name: knowledge graph
on: [push]
jobs:
  graph:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: neko233-com/graphify233@main
        with:
          path: .
          mode: deep            # optional
          version: latest       # or pin e.g. v0.5.0
          upload-artifact: "true"
```

Outputs are `graph-json` and `summary`; the artifact contains graph.json,
GRAPH_REPORT.md, graph.html, obsidian/ and wiki/. For private networks pin
`version` and use the mirror environment variables from the install section.

## Languages

Code: Python, Go, JavaScript/TypeScript, Rust, Java, C, C++, Ruby, C#,
Kotlin, Scala, PHP. Config/infra: SQL DDL (tables, views, indexes, foreign
keys), Terraform/HCL (resources, data, modules, variables, outputs), Dockerfile
(build stages and `COPY --from`), OpenAPI/Swagger JSON and YAML (API + one node
per path/method). Documents: Markdown, plain text, reStructuredText. Papers:
PDF (built-in heuristic text extraction). Images: png, jpg, webp, gif, svg
(recorded as assets).

Extraction is deterministic and every edge records where it came from:

- `EXTRACTED` — explicit in the source (definitions, imports, inheritance).
- `INFERRED` — derived (call graph, cross-file imports).
- `AMBIGUOUS` — uncertain and worth reviewing.

Nodes carry `source_file` and `source_location`; edges carry `source_file`,
`source_location`, `confidence` and `confidence_score`.

## MCP and HTTP

```sh
graphify233 mcp .                                   # stdio MCP server
graphify233 serve-http --token s3cret               # 127.0.0.1:7331
curl -H "Authorization: Bearer s3cret" "localhost:7331/query?q=clustering"
```

MCP tools: `query_graph`, `get_node`, `get_neighbors`, `get_community`,
`god_nodes`, `graph_stats`, `shortest_path`. The HTTP server exposes the same
graph as JSON: `/stats`, `/god`, `/query` (GET or POST), `/node`, `/neighbors`,
`/path`, and `/mcp` for streamable JSON-RPC. Every route except `/healthz`
requires `Authorization: Bearer <token>` when a token is set; the token defaults
to `$GRAPHIFY233_HTTP_TOKEN`.

## Analysis and diff

`graphify233 analyze` reports dependency cycles (Tarjan SCC over calls/uses/
references/inheritance), unreferenced symbols (no incoming dependency edge,
entry points excluded) and hubs. `graphify233 diff old.json new.json --impact`
lists added/removed nodes and edges, nodes that moved between communities, and
for each change the reverse dependency set ("what breaks if this changes").

## Performance

Measured on this machine (Go 1.27.3, Windows/amd64, warm page cache):

| Corpus | Files | Result | Time |
| --- | --- | --- | --- |
| Go `net/http` | 158 | 4,045 nodes · 8,178 edges · 104 communities | 491 ms full build |
| Go standard library | 9,701 | 114,099 nodes · 188,135 edges · 5,360 communities | 5.7 s cold, 2.6 s incremental |
| Mixed Python project | 19 | 148 nodes · 223 edges · 15 communities | 33 ms |

For scale, the Python reference's tree-sitter extraction over the same
`net/http` corpus takes **719 ms** for 158 files; `graphify233` spends **40 ms**
extracting, and the remaining 451 ms is clustering, HTML/SVG/JSON generation and
the Obsidian export. That is roughly an 18x speedup on the extraction stage, and
the whole pipeline still finishes in under half a second.

- Parallel file extraction (worker pool, one lexer pass per file).
- Content-hash cache: unchanged files are never re-parsed; `--update` also
  short-circuits on size+mtime.
- Streaming SHA-256, buffered JSON encoding, compact in-memory graph with
  adjacency built once.
- Deterministic Louvain clustering with oversized-community splitting.
- Single static binary (~8 MB); no external tools invoked.

## Security

- `self-update` refuses to run unless the release's Ed25519 signature over
  `checksums.txt` verifies **before** any hash is trusted, then verifies the
  binary's SHA-256 and installs atomically next to the running executable.
- Asset downloads fall back from the release CDN to the release-asset API when
  the former fails; both routes are covered by the same signature + checksum
  verification.
- `GRAPHIFY233_MIRROR` prefixes asset URLs only; signatures are still verified
  locally, so a malicious mirror cannot inject a binary.
- Sensitive files (`.env`, private keys, credential files) are skipped and
  reported, never parsed.
- The private signing key (`release/keys/ed25519.key`) is gitignored.

## Development

```sh
go test ./...                 # golden-parity extraction tests + unit tests
go vet ./...                  # static checks
bash scripts/e2e.sh           # build→query→analyze→diff→HTTP→MCP (+E2E_WATCH=1)
./release.ps1 -Version v0.5.0 # or ./release.sh v0.5.0
./release.ps1 -Publish -Version v0.5.0   # gh release create
```

Tests cover extraction parity, clustering (including the multi-level modularity
regression), analysis (impact, cycles, unreferenced symbols, community drift),
exports (JSON round-trip, Cypher, GraphML, SVG, Canvas), query, MCP, the HTTP
API, hooks, installation, doc/code mentions, concepts, the semantic layer
(concept similarity and an `httptest` embeddings endpoint), new extractors
(SQL/Terraform/Dockerfile/OpenAPI), and the incremental pipeline (cache reuse,
sensitive-file exclusion).

Extraction is verified against the Python reference's own output:
`tools/gen_goldens.py` regenerates `internal/extract/testdata/expected_*.json`
and the golden test compares node and edge order, ids, labels, locations,
relations and confidences for all 13 languages.

Packages: `internal/lex` (tokenizer), `internal/extract` (per-language
scanners), `internal/detect`, `internal/pdf`, `internal/graphbuild`,
`internal/cluster`, `internal/analyze`, `internal/enrich` (mentions, concepts),
`internal/semantic` (opt-in similarity), `internal/report`, `internal/export`,
`internal/cache`, `internal/pipeline`, `internal/query`, `internal/benchmark`,
`internal/mcp`, `internal/httpapi`, `internal/hooks`, `internal/install`,
`internal/selfupdate`.
