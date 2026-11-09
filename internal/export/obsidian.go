package export

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

// WriteObsidian writes one Markdown note per node plus a community overview,
// wired together with wikilinks. Returns the number of notes written.
func WriteObsidian(dir string, g *model.Graph, comms [][]int32, labels map[int]string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	used := make(map[string]int, len(g.Nodes))
	names := make([]string, len(g.Nodes))
	for i, n := range g.Nodes {
		base := safeName(n.Label)
		if base == "" {
			base = safeName(n.ID)
		}
		if base == "" {
			base = "node"
		}
		name := base
		for k := used[base]; k > 0; k = used[base] {
			used[base]++
			name = fmt.Sprintf("%s %d", base, used[base])
			if _, taken := used[name]; !taken {
				break
			}
		}
		used[name] = 1
		names[i] = name
	}

	adj := g.Adj()
	written := 0
	for i, n := range g.Nodes {
		rel := make([]struct {
			other string
			rel   string
			conf  string
		}, 0, len(adj[i]))
		for _, e := range g.Edges {
			if e.Source == n.ID && e.Target != n.ID {
				if j := g.Index(e.Target); j >= 0 {
					rel = append(rel, struct {
						other string
						rel   string
						conf  string
					}{names[j], e.Relation, e.Confidence})
				}
			} else if e.Target == n.ID && e.Source != n.ID {
				if j := g.Index(e.Source); j >= 0 {
					rel = append(rel, struct {
						other string
						rel   string
						conf  string
					}{names[j], e.Relation, e.Confidence})
				}
			}
		}
		sort.SliceStable(rel, func(a, b int) bool { return rel[a].other < rel[b].other })
		conf := dominantConfidence(g, n.ID)
		commName := labels[n.Community]
		if commName == "" {
			commName = fmt.Sprintf("Community %d", n.Community)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "---\nsource_file: %q\ntype: %s\ncommunity: %d\n", n.SourceFile, n.FileType, n.Community)
		if n.SourceLocation != "" {
			fmt.Fprintf(&b, "location: %q\n", n.SourceLocation)
		}
		fmt.Fprintf(&b, "tags:\n  - graphify/%s\n  - graphify/%s\n  - community/%s\n---\n\n", n.FileType, conf, safeTag(commName))
		fmt.Fprintf(&b, "# %s\n\n", n.Label)
		if n.SourceFile != "" {
			fmt.Fprintf(&b, "`%s` %s\n\n", n.SourceFile, n.SourceLocation)
		}
		if len(rel) > 0 {
			b.WriteString("## Connections\n\n")
			for _, r := range rel {
				fmt.Fprintf(&b, "- [[%s]] - %s (%s)\n", r.other, r.rel, r.conf)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, names[i]+".md"), []byte(b.String()), 0o644); err != nil {
			return written, err
		}
		written++
	}

	for ci, c := range comms {
		name := labels[ci]
		if name == "" {
			name = fmt.Sprintf("Community %d", ci)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "---\ntype: community\ncommunity: %d\n---\n\n# %s\n\n", ci, name)
		fmt.Fprintf(&b, "**%d nodes** · cohesion %.2f\n\n## Members\n\n", len(c), cohesionSafe(g, c))
		sort.SliceStable(c, func(i, j int) bool { return g.Nodes[c[i]].Label < g.Nodes[c[j]].Label })
		for _, m := range c {
			fmt.Fprintf(&b, "- [[%s]]\n", names[m])
		}
		if err := os.WriteFile(filepath.Join(dir, safeName("_COMMUNITY_"+name)+".md"), []byte(b.String()), 0o644); err != nil {
			return written, err
		}
	}
	return written, nil
}

func cohesionSafe(g *model.Graph, members []int32) float64 {
	stamp := make(map[int32]struct{}, len(members))
	for _, m := range members {
		stamp[m] = struct{}{}
	}
	if len(members) < 2 {
		return 0
	}
	edges := 0
	for _, m := range members {
		for _, nb := range g.Adj()[m] {
			if _, ok := stamp[nb]; ok {
				edges++
			}
		}
	}
	edges /= 2
	return float64(edges) / (float64(len(members)) * float64(len(members)-1) / 2)
}

func dominantConfidence(g *model.Graph, id string) string {
	best, bestN := model.ConfExtracted, -1
	counts := map[string]int{}
	for _, e := range g.Edges {
		if e.Source == id || e.Target == id {
			counts[e.Confidence]++
		}
	}
	for _, c := range []string{model.ConfExtracted, model.ConfInferred, model.ConfAmbiguous} {
		if counts[c] > bestN {
			bestN = counts[c]
			best = c
		}
	}
	return best
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '#', '^', '[', ']', '\n', '\r', '\t':
			if r == '/' {
				b.WriteByte('-')
			}
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(strings.Trim(b.String(), "."))
	if len(out) > 120 {
		out = strings.TrimSpace(out[:120])
	}
	return out
}

func safeTag(s string) string {
	return strings.ReplaceAll(safeName(s), " ", "_")
}

// WriteObsidianIndex renders a small entry note pointing at the communities.
func WriteObsidianIndex(dir string, g *model.Graph, labels map[int]string, comms [][]int32) error {
	var b strings.Builder
	b.WriteString("---\ntype: index\n---\n\n# Knowledge Graph\n\n")
	fmt.Fprintf(&b, "%d nodes · %d edges · %d communities\n\n## Communities\n\n", len(g.Nodes), len(g.Edges), len(comms))
	for ci, c := range comms {
		name := labels[ci]
		if name == "" {
			name = fmt.Sprintf("Community %d", ci)
		}
		fmt.Fprintf(&b, "- [[_COMMUNITY_%s]] - %d nodes\n", name, len(c))
	}
	f, err := os.Create(filepath.Join(dir, "_INDEX.md"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	w.WriteString(b.String())
	return w.Flush()
}
