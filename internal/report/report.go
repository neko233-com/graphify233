// Package report renders GRAPH_REPORT.md, the human-readable summary of a run.
package report

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neko233-com/graphify233/internal/analyze"
	"github.com/neko233-com/graphify233/internal/detect"
	"github.com/neko233-com/graphify233/internal/model"
)

// Data is everything the report needs.
type Data struct {
	Root         string
	Graph        *model.Graph
	Communities  [][]int32
	Cohesion     []float64
	Labels       map[int]string
	GodNodes     []analyze.GodNode
	Surprises    []analyze.Surprise
	Questions    []string
	Detection    *detect.Result
	Mode         string
	InputTokens  int
	OutputTokens int
}

// Write renders the report to path.
func Write(path string, d Data) error {
	return os.WriteFile(path, []byte(Render(d)), 0o644)
}

// Render returns the Markdown report.
func Render(d Data) string {
	g := d.Graph
	var b strings.Builder
	root := d.Root
	if root == "" {
		root = "."
	}
	fmt.Fprintf(&b, "# Graph Report - %s  (%s)\n\n", root, time.Now().Format("2006-01-02"))

	b.WriteString("## Corpus Check\n")
	if d.Detection != nil {
		fmt.Fprintf(&b, "- %d files · ~%s words\n", d.Detection.TotalFiles, human(d.Detection.TotalWords))
		if d.Detection.Warning != "" {
			fmt.Fprintf(&b, "- ⚠️ %s\n", d.Detection.Warning)
		}
		if n := len(d.Detection.SkippedSensitive); n > 0 {
			fmt.Fprintf(&b, "- %d sensitive file(s) skipped\n", n)
		}
	}
	b.WriteString("\n")

	b.WriteString("## Summary\n")
	fmt.Fprintf(&b, "- %d nodes · %d edges · %d communities\n", len(g.Nodes), len(g.Edges), len(d.Communities))
	extracted, inferred, ambiguous := confidenceCounts(g)
	total := len(g.Edges)
	if total > 0 {
		fmt.Fprintf(&b, "- Extraction: %s%% EXTRACTED · %s%% INFERRED · %s%% AMBIGUOUS\n",
			pct(extracted, total), pct(inferred, total), pct(ambiguous, total))
	}
	if d.InputTokens > 0 || d.OutputTokens > 0 {
		fmt.Fprintf(&b, "- Token cost: %s input · %s output\n", human(d.InputTokens), human(d.OutputTokens))
	}
	if d.Mode != "" {
		fmt.Fprintf(&b, "- Mode: %s\n", d.Mode)
	}
	b.WriteString("\n")

	b.WriteString("## God Nodes (most connected - your core abstractions)\n")
	if len(d.GodNodes) == 0 {
		b.WriteString("- None - the graph has no connected non-file nodes.\n")
	}
	for i, gn := range d.GodNodes {
		fmt.Fprintf(&b, "%d. `%s` - %d edges\n", i+1, gn.Label, gn.Edges)
	}
	b.WriteString("\n")

	b.WriteString("## Surprising Connections (you probably didn't know these)\n")
	if len(d.Surprises) == 0 {
		b.WriteString("- None detected - all connections are within the same community or file.\n")
	}
	for _, s := range d.Surprises {
		fmt.Fprintf(&b, "- `%s` --%s--> `%s`  [%s]\n", s.SourceLabel, s.Relation, s.TargetLabel, s.Confidence)
		if s.SourceFile != "" && s.SourceFile != s.TargetFile {
			fmt.Fprintf(&b, "  %s → %s", s.SourceFile, s.TargetFile)
		}
		if len(s.Why) > 0 {
			if s.SourceFile != "" && s.SourceFile != s.TargetFile {
				b.WriteString("  ")
			}
			b.WriteString("_" + strings.Join(s.Why, "; ") + "_")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(g.Hyperedges) > 0 {
		b.WriteString("## Hyperedges (group relationships)\n")
		for _, h := range g.Hyperedges {
			fmt.Fprintf(&b, "- **%s** — %s\n", h.Label, strings.Join(h.Nodes, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Communities\n")
	for i, c := range d.Communities {
		label := d.Labels[i]
		if label == "" {
			label = "Community " + strconv.Itoa(i)
		}
		fmt.Fprintf(&b, "### Community %d - \"%s\"\n", i, label)
		if i < len(d.Cohesion) {
			fmt.Fprintf(&b, "Cohesion: %.2f\n", d.Cohesion[i])
		}
		names := make([]string, 0, 12)
		shown := 0
		for _, m := range c {
			n := g.Nodes[m]
			if g.IsFileNode(n) || model.IsConceptNode(n) {
				continue
			}
			if shown >= 12 {
				names = append(names, fmt.Sprintf("(+%d more)", len(c)-shown))
				break
			}
			names = append(names, n.Label)
			shown++
		}
		if len(names) == 0 {
			for _, m := range c {
				if shown >= 12 {
					names = append(names, fmt.Sprintf("(+%d more)", len(c)-shown))
					break
				}
				names = append(names, g.Nodes[m].Label)
				shown++
			}
		}
		fmt.Fprintf(&b, "Nodes (%d): %s\n\n", len(c), strings.Join(names, ", "))
	}

	b.WriteString("## Ambiguous Edges - Review These\n")
	ambiguousLines := 0
	for _, e := range g.Edges {
		if e.Confidence != model.ConfAmbiguous {
			continue
		}
		s, t := g.Index(e.Source), g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		fmt.Fprintf(&b, "- `%s` → `%s` [%s] · %s%s\n", g.Nodes[s].Label, g.Nodes[t].Label, e.Relation, e.SourceFile, e.SourceLocation)
		ambiguousLines++
		if ambiguousLines >= 10 {
			break
		}
	}
	if ambiguousLines == 0 {
		b.WriteString("- None - every edge has a clear source.\n")
	}
	b.WriteString("\n")

	b.WriteString("## Knowledge Gaps\n")
	gaps := knowledgeGaps(g, d)
	if len(gaps) == 0 {
		b.WriteString("- None detected.\n")
	}
	for _, gap := range gaps {
		fmt.Fprintf(&b, "- %s\n", gap)
	}
	b.WriteString("\n")

	b.WriteString("## Suggested Questions\n")
	for _, q := range d.Questions {
		fmt.Fprintf(&b, "- %s\n", q)
	}
	return b.String()
}

func knowledgeGaps(g *model.Graph, d Data) []string {
	var gaps []string
	adj := g.Adj()

	isolated := 0
	samples := 0
	var isolatedNames []string
	for i, n := range g.Nodes {
		if len(adj[i]) == 0 {
			isolated++
			if samples < 5 {
				isolatedNames = append(isolatedNames, n.Label)
				samples++
			}
		}
	}
	if isolated > 0 {
		gaps = append(gaps, fmt.Sprintf("**%d isolated node(s):** `%s` (not connected to anything)", isolated, strings.Join(isolatedNames, "`, `")))
	}

	thin := 0
	for _, c := range d.Communities {
		if len(c) < 3 {
			thin++
		}
	}
	if thin > 0 {
		gaps = append(gaps, fmt.Sprintf("**%d thin communities (size < 3)** - likely incomplete extractions", thin))
	}

	if len(g.Edges) > 0 {
		_, _, amb := confidenceCounts(g)
		ratio := float64(amb) / float64(len(g.Edges))
		if ratio > 0.2 {
			gaps = append(gaps, fmt.Sprintf("**High ambiguity:** %.0f%% of edges are AMBIGUOUS - consider `--mode deep`", ratio*100))
		}
	}
	return gaps
}

func confidenceCounts(g *model.Graph) (extracted, inferred, ambiguous int) {
	for _, e := range g.Edges {
		switch e.Confidence {
		case model.ConfInferred:
			inferred++
		case model.ConfAmbiguous:
			ambiguous++
		default:
			extracted++
		}
	}
	return
}

func pct(n, total int) string {
	return strconv.FormatFloat(float64(n)*100/float64(total), 'f', 0, 64)
}

func human(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// SortCommunityNodes orders community members by degree for stable display.
func SortCommunityNodes(g *model.Graph, c []int32) {
	sort.SliceStable(c, func(i, j int) bool {
		return g.Degree(c[i]) > g.Degree(c[j])
	})
}
