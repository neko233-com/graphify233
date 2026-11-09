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

The release lookup and the asset downloads can both go through mirrors. The
Ed25519 signature is always verified locally, so a mirror cannot inject a
binary or a forged checksum file.

```powershell
$env:GRAPHIFY233_API = "https://api.kkgithub.com"   # GitHub API mirror
$env:GRAPHIFY233_MIRROR = "https://ghfast.top"      # prefixes github.com URLs
irm https://raw.githubusercontent.com/neko233-com/graphify233/main/install.ps1 | iex
```

```sh
GRAPHIFY233_API=https://api.kkgithub.com \
GRAPHIFY233_MIRROR=https://ghfast.top \
  curl -fsSL https://raw.githubusercontent.com/neko233-com/graphify233/main/install.sh | bash
```

`graphify233 self-update` honors the same two variables.

## Quick start
```sh
graphify233 build .              # build graphify-out/ for the current project
graphify233 query "how does auth work"
graphify233 path detect extract  # shortest path between two symbols
graphify233 explain extract      # symbol detail and connections
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
graphify233 explain NAME            symbol detail
graphify233 path FROM TO            shortest path
graphify233 benchmark [dir]         token reduction report
graphify233 mcp [dir]               MCP server over stdio
graphify233 watch [dir]             rebuild on file changes
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
--no-obsidian`, `--neo4j URL --neo4j-user --neo4j-pass`, `--quiet`.

`--mode deep` never calls an LLM: extraction stays deterministic, and the flag
widens the analysis (more god nodes and surprising connections) and links
documentation to code. In deep mode every Markdown/text/PDF file is scanned for
identifiers that match a uniquely named code symbol; each hit becomes an
INFERRED `mentions` edge with a source line, so questions like "how does
authentication work" can start from the docs. `graphify233 query` falls back to
those mentions when no symbol name matches directly.

`graphify233 install claude` writes the Claude Code skill and slash command into
`~/.claude/`, and adds a short `<!-- graphify233:start -->` section to the
project's `CLAUDE.md` and `.github/copilot-instructions.md`. Existing content is
preserved, re-running refreshes only that section, and `install uninstall`
removes it again. `install vscode` merges a build task into `.vscode/tasks.json`.

## Languages

Code: Python, Go, JavaScript/TypeScript, Rust, Java, C, C++, Ruby, C#,
Kotlin, Scala, PHP. Documents: Markdown, plain text, reStructuredText. Papers:
PDF (built-in heuristic text extraction). Images: png, jpg, webp, gif, svg
(recorded as assets).

Extraction is deterministic and every edge records where it came from:

- `EXTRACTED` — explicit in the source (definitions, imports, inheritance).
- `INFERRED` — derived (call graph, cross-file imports).
- `AMBIGUOUS` — uncertain and worth reviewing.

Nodes carry `source_file` and `source_location`; edges carry `source_file`,
`source_location`, `confidence` and `confidence_score`.

## MCP

```sh
graphify233 mcp .        # add as a stdio MCP server in Claude Code / any client
```

Tools: `query_graph`, `get_node`, `get_neighbors`, `get_community`,
`god_nodes`, `graph_stats`, `shortest_path`.

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
- `GRAPHIFY233_MIRROR` prefixes asset URLs only; signatures are still verified
  locally, so a malicious mirror cannot inject a binary.
- Sensitive files (`.env`, private keys, credential files) are skipped and
  reported, never parsed.
- The private signing key (`release/keys/ed25519.key`) is gitignored.

## Development

```sh
go test ./...                 # golden-parity extraction tests + unit tests
go vet ./...                  # static checks
./release.ps1 -Version v0.2.0 # or ./release.sh v0.2.0
./release.ps1 -Publish -Version v0.2.0   # gh release create
```

Tests cover extraction parity, clustering (including the multi-level modularity
regression), analysis, exports (JSON round-trip, Cypher, GraphML, SVG, Canvas),
query, MCP, hooks, installation, doc/code mentions, and the incremental
pipeline (cache reuse, sensitive-file exclusion).

Extraction is verified against the Python reference's own output:
`tools/gen_goldens.py` regenerates `internal/extract/testdata/expected_*.json`
and the golden test compares node and edge order, ids, labels, locations,
relations and confidences for all 13 languages.

Packages: `internal/lex` (tokenizer), `internal/extract` (per-language
scanners), `internal/detect`, `internal/pdf`, `internal/graphbuild`,
`internal/cluster`, `internal/analyze`, `internal/report`,
`internal/export`, `internal/cache`, `internal/pipeline`, `internal/query`,
`internal/benchmark`, `internal/mcp`, `internal/hooks`, `internal/install`,
`internal/selfupdate`.
