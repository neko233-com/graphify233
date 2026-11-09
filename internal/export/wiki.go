package export

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/analyze"
	"github.com/neko233-com/graphify233/internal/model"
)

// WriteWiki writes a navigable wiki: an index, one article per community and
// one article per god node. It mirrors the reference wiki layout.
func WriteWiki(dir string, g *model.Graph, comms [][]int32, labels map[int]string, cohesion []float64, gods []analyze.GodNode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, len(comms))
	used := map[string]bool{}
	for ci := range comms {
		name := labels[ci]
		if name == "" {
			name = fmt.Sprintf("Community %d", ci)
		}
		base := wikiFileName(name)
		final := base
		for k := 2; used[final]; k++ {
			final = fmt.Sprintf("%s_%d", base, k)
		}
		used[final] = true
		names[ci] = final
	}

	memberOf := make([]int, len(g.Nodes))
	for i := range memberOf {
		memberOf[i] = -1
	}
	for ci, c := range comms {
		for _, m := range c {
			memberOf[m] = ci
		}
	}

	var idx strings.Builder
	idx.WriteString("# Knowledge Graph Index\n\n")
	fmt.Fprintf(&idx, "%d nodes · %d edges · %d communities\n\n", len(g.Nodes), len(g.Edges), len(comms))
	idx.WriteString("## Communities\n\n")
	for ci, c := range comms {
		co := 0.0
		if ci < len(cohesion) {
			co = cohesion[ci]
		}
		fmt.Fprintf(&idx, "- [[%s]] — %d nodes, cohesion %.2f\n", names[ci], len(c), co)
	}
	idx.WriteString("\n## God Nodes\n\n")
	for _, gn := range gods {
		fmt.Fprintf(&idx, "- [[%s]] — %d connections\n", wikiFileName(gn.Label), gn.Edges)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(idx.String()), 0o644); err != nil {
		return err
	}

	// Source files per community and cross-community edges.
	cross := make([][]int, len(comms))
	for ci := range comms {
		cross[ci] = make([]int, len(comms))
	}
	for _, e := range g.Edges {
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		cs, ct := memberOf[s], memberOf[t]
		if cs >= 0 && ct >= 0 && cs != ct {
			cross[cs][ct]++
			cross[ct][cs]++
		}
	}

	for ci, c := range comms {
		co := 0.0
		if ci < len(cohesion) {
			co = cohesion[ci]
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", labels[ci])
		fmt.Fprintf(&b, "> %d nodes · cohesion %.2f\n\n", len(c), co)
		b.WriteString("## Key Concepts\n\n")
		ordered := append([]int32(nil), c...)
		sort.SliceStable(ordered, func(i, j int) bool {
			di, dj := g.Degree(ordered[i]), g.Degree(ordered[j])
			if di != dj {
				return di > dj
			}
			return g.Nodes[ordered[i]].Label < g.Nodes[ordered[j]].Label
		})
		limit := 25
		if len(ordered) < limit {
			limit = len(ordered)
		}
		for _, m := range ordered[:limit] {
			n := g.Nodes[m]
			src := n.SourceFile
			if src == "" {
				src = "external"
			}
			fmt.Fprintf(&b, "- **%s** (%d connections) — `%s`\n", n.Label, g.Degree(m), src)
		}
		b.WriteString("\n")

		b.WriteString("## Relationships\n\n")
		type rel struct {
			ci int
			n  int
		}
		var rels []rel
		for oi, n := range cross[ci] {
			if oi != ci && n > 0 {
				rels = append(rels, rel{ci: oi, n: n})
			}
		}
		sort.SliceStable(rels, func(i, j int) bool { return rels[i].n > rels[j].n })
		if len(rels) == 0 {
			b.WriteString("_No cross-community relationships._\n\n")
		} else {
			if len(rels) > 12 {
				rels = rels[:12]
			}
			for _, r := range rels {
				name := labels[r.ci]
				if name == "" {
					name = fmt.Sprintf("Community %d", r.ci)
				}
				fmt.Fprintf(&b, "- [[%s]] (%d shared connections)\n", wikiFileName(name), r.n)
			}
			b.WriteString("\n")
		}

		b.WriteString("## Source Files\n\n")
		fileCount := make(map[string]int, 8)
		for _, m := range c {
			if f := g.Nodes[m].SourceFile; f != "" {
				fileCount[f]++
			}
		}
		type fc struct {
			file string
			n    int
		}
		var files []fc
		for f, n := range fileCount {
			files = append(files, fc{file: f, n: n})
		}
		sort.SliceStable(files, func(i, j int) bool {
			if files[i].n != files[j].n {
				return files[i].n > files[j].n
			}
			return files[i].file < files[j].file
		})
		if len(files) > 20 {
			files = files[:20]
		}
		if len(files) == 0 {
			b.WriteString("_No source files._\n\n")
		}
		for _, f := range files {
			fmt.Fprintf(&b, "- `%s` (%d nodes)\n", f.file, f.n)
		}
		b.WriteString("\n")

		b.WriteString("## Audit Trail\n\n")
		ext, inf, amb := communityConfidence(g, c)
		total := ext + inf + amb
		if total > 0 {
			fmt.Fprintf(&b, "- EXTRACTED: %d (%.0f%%)\n", ext, float64(ext)*100/float64(total))
			fmt.Fprintf(&b, "- INFERRED: %d (%.0f%%)\n", inf, float64(inf)*100/float64(total))
			fmt.Fprintf(&b, "- AMBIGUOUS: %d (%.0f%%)\n", amb, float64(amb)*100/float64(total))
		} else {
			b.WriteString("- No internal edges.\n")
		}
		fmt.Fprintf(&b, "\n> Part of [[index]]\n")

		if err := os.WriteFile(filepath.Join(dir, names[ci]+".md"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	for _, gn := range gods {
		idx := g.Index(gn.ID)
		if idx < 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", gn.Label)
		src := g.Nodes[idx].SourceFile
		if src == "" {
			src = "external"
		}
		fmt.Fprintf(&b, "> God node · %d connections · `%s`\n\n", gn.Edges, src)
		if ci := memberOf[idx]; ci >= 0 {
			fmt.Fprintf(&b, "**Community:** [[%s]]\n\n", names[ci])
		}
		b.WriteString("## Connections by Relation\n\n")
		byRel := make(map[string][]string, 8)
		for _, e := range g.Edges {
			var other string
			if e.Source == gn.ID {
				if j := g.Index(e.Target); j >= 0 {
					other = g.Nodes[j].Label
				}
			} else if e.Target == gn.ID {
				if j := g.Index(e.Source); j >= 0 {
					other = g.Nodes[j].Label
				}
			}
			if other != "" {
				byRel[e.Relation] = append(byRel[e.Relation], other)
			}
		}
		rels := make([]string, 0, len(byRel))
		for r := range byRel {
			rels = append(rels, r)
		}
		sort.Strings(rels)
		for _, r := range rels {
			others := byRel[r]
			sort.Strings(others)
			if len(others) > 20 {
				others = others[:20]
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", r, strings.Join(others, ", "))
		}
		if err := os.WriteFile(filepath.Join(dir, wikiFileName(gn.Label)+".md"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func communityConfidence(g *model.Graph, members []int32) (ext, inf, amb int) {
	stamp := make(map[int32]struct{}, len(members))
	for _, m := range members {
		stamp[m] = struct{}{}
	}
	for _, e := range g.Edges {
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		if _, ok := stamp[s]; !ok {
			continue
		}
		if _, ok := stamp[t]; !ok {
			continue
		}
		switch e.Confidence {
		case model.ConfInferred:
			inf++
		case model.ConfAmbiguous:
			amb++
		default:
			ext++
		}
	}
	return
}

func wikiFileName(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "untitled"
	}
	return s
}
