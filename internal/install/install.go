// Package install writes editor integrations (Claude Code skill + slash
// command, project guidance files, VS Code task) that teach assistants how to
// drive the CLI.
package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

const (
	sectionStart = "<!-- graphify233:start -->"
	sectionEnd   = "<!-- graphify233:end -->"
)

// guidance is injected into project-level instruction files so assistants know
// the graph exists and how to use it.
const guidance = `## graphify233

This project has a local knowledge graph in ` + "`graphify-out/`" + `.

- Rebuild after edits: ` + "`graphify233 build . --update`" + `
- Answer architecture questions with ` + "`graphify233 query \"...\"`" + ` instead of broad greps.
- Look up a symbol with ` + "`graphify233 explain Name`" + `, trace it with ` + "`graphify233 path A B`" + `.
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

// GuidanceFiles lists the project files that receive the graphify233 section.
func GuidanceFiles(root string) []string {
	if root == "" {
		root = "."
	}
	return []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, ".github", "copilot-instructions.md"),
	}
}

// WriteGuidance inserts (or refreshes) the graphify233 section in CLAUDE.md and
// .github/copilot-instructions.md. Existing content is preserved.
func WriteGuidance(root string, out io.Writer) ([]string, error) {
	block := sectionStart + "\n" + guidance + sectionEnd + "\n"
	var written []string
	for _, p := range GuidanceFiles(root) {
		cur, err := os.ReadFile(p)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		text := string(cur)
		if start := strings.Index(text, sectionStart); start >= 0 {
			if end := strings.Index(text[start:], sectionEnd); end >= 0 {
				text = text[:start] + block + text[start+end+len(sectionEnd):]
				text = strings.TrimPrefix(text, "\n")
			} else {
				text = text[:start] + block
			}
		} else {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if text != "" {
				text += "\n"
			}
			text += block
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return nil, err
		}
		written = append(written, p)
		if out != nil {
			fmt.Fprintf(out, "wrote guidance: %s\n", p)
		}
	}
	return written, nil
}

// RemoveGuidance strips the graphify233 section from the project files.
func RemoveGuidance(root string, out io.Writer) error {
	for _, p := range GuidanceFiles(root) {
		cur, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		text := string(cur)
		start := strings.Index(text, sectionStart)
		if start < 0 {
			continue
		}
		end := strings.Index(text[start:], sectionEnd)
		if end < 0 {
			text = text[:start]
		} else {
			text = text[:start] + text[start+end+len(sectionEnd):]
		}
		text = strings.TrimRight(text, "\n")
		if text != "" {
			text += "\n"
		}
		if strings.TrimSpace(text) == "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return err
		}
		if out != nil {
			fmt.Fprintf(out, "cleaned %s\n", p)
		}
	}
	return nil
}

type vscodeFile struct {
	Version string           `json:"version"`
	Tasks   []map[string]any `json:"tasks"`
}

const vscodeTaskLabel = "graphify233: build knowledge graph"

// VSCodeTask adds a build task to .vscode/tasks.json without disturbing
// existing tasks. The file must be strict JSON; JSONC keeps its comments and is
// left untouched with an explanatory error.
func VSCodeTask(root string, out io.Writer) (string, error) {
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, ".vscode", "tasks.json")
	doc := vscodeFile{Version: "2.0.0", Tasks: []map[string]any{}}
	cur, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(cur, &doc); err != nil {
			return "", fmt.Errorf("%s is not strict JSON (%v); add the task manually", path, err)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, t := range doc.Tasks {
		if label, _ := t["label"].(string); label == vscodeTaskLabel {
			if out != nil {
				fmt.Fprintf(out, "task already present: %s\n", path)
			}
			return path, nil
		}
	}
	if doc.Version == "" {
		doc.Version = "2.0.0"
	}
	doc.Tasks = append(doc.Tasks, map[string]any{
		"label":          vscodeTaskLabel,
		"type":           "shell",
		"command":        "graphify233",
		"args":           []string{"build", ".", "--update"},
		"group":          "build",
		"problemMatcher": []string{},
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return "", err
	}
	if out != nil {
		fmt.Fprintf(out, "wrote task: %s\n", path)
	}
	return path, nil
}
