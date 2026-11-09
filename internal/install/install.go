// Package install writes editor integrations (Claude Code skill + slash
// command) that teach assistants how to drive the CLI.
package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SkillDoc is the Claude Code skill body.
const SkillDoc = `---
name: graphify233
description: Build and query a local knowledge graph of a codebase. Use when the user asks about architecture, how components connect, where something is defined, or to explore an unfamiliar repository.
---

# graphify233

graphify233 turns a directory into a deterministic, queryable knowledge graph
stored in ` + "`graphify-out/`" + `. Everything runs locally; no API keys.

## Build

` + "```bash\ngraphify233 .                       # build graph.json + report + HTML + Obsidian\ngraphify233 update .                # incremental rebuild (only changed files)\ngraphify233 . --wiki --svg --graphml --cypher --canvas\n```" + `

## Query

` + "```bash\ngraphify233 query \"how does authentication work\"\ngraphify233 path Handler Store\ngraphify233 explain ParseConfig\ngraphify233 benchmark        # token-reduction estimate\n```" + `

## Outputs (graphify-out/)

- ` + "`graph.json`" + ` - node-link graph, every edge tagged EXTRACTED / INFERRED / AMBIGUOUS
- ` + "`GRAPH_REPORT.md`" + ` - summary, god nodes, surprising connections, suggested questions
- ` + "`graph.html`" + ` - self-contained interactive viewer
- ` + "`obsidian/`" + ` - one note per node with wikilinks

## MCP

` + "```json\n{\"mcpServers\":{\"graphify233\":{\"command\":\"graphify233\",\"args\":[\"mcp\"]}}}\n```" + `

Prefer ` + "`graphify233 query`" + ` over grepping a large repository: it returns a small,
high-signal subgraph with source locations.
`

// CommandDoc is the Claude Code slash command body.
const CommandDoc = `---
description: Build or query the local code knowledge graph
---

Run ` + "`graphify233`" + ` for the current directory:

- Build: ` + "`graphify233 .`" + `
- Query: ` + "`graphify233 query \"$ARGUMENTS\"`" + `
- If ` + "`graphify-out/graph.json`" + ` is missing, build first.
- Summarise the returned nodes, their source files and the relations between them.
`

// Paths reports the files Install would write.
func Paths() (skill, command string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".claude", "skills", "graphify233", "SKILL.md"),
		filepath.Join(home, ".claude", "commands", "graphify233.md"), nil
}

// Claude writes the skill and slash command, returning the written paths.
func Claude(out io.Writer) ([]string, error) {
	skill, command, err := Paths()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(skill, []byte(SkillDoc), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(command, []byte(CommandDoc), 0o644); err != nil {
		return nil, err
	}
	if out != nil {
		fmt.Fprintf(out, "installed skill: %s\n", skill)
		fmt.Fprintf(out, "installed command: %s\n", command)
	}
	return []string{skill, command}, nil
}

// RemoveClaude deletes the files written by Claude.
func RemoveClaude(out io.Writer) error {
	skill, command, err := Paths()
	if err != nil {
		return err
	}
	for _, p := range []string{skill, command} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		if out != nil {
			fmt.Fprintf(out, "removed %s\n", p)
		}
	}
	return nil
}
