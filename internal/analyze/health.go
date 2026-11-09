package analyze

import (
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

// healthRelations are the relations that express a real dependency edge for
// cycle detection; structural containment would make every file a cycle.
var healthRelations = map[string]bool{
	"calls": true, "uses": true, "references": true, "inherits": true,
	"implements": true, "imports_from": true,
}

// Cycles returns strongly connected components with more than one node, i.e.
// dependency cycles between symbols.
func Cycles(g *model.Graph) [][]string {
	n := len(g.Nodes)
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []int32
	var out [][]string
	next := 1
	var strong func(v int32)
	strong = func(v int32) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, e := range g.Edges {
			if !healthRelations[e.Relation] {
				continue
			}
			s, t := g.Index(e.Source), g.Index(e.Target)
			if s != v || t < 0 {
				continue
			}
			w := t
			if index[w] == -1 {
				strong(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[top] = false
				if g.Nodes[top].FileType == "" || g.Nodes[top].FileType == model.FileTypeCode {
					comp = append(comp, g.Nodes[top].Label)
				}
				if top == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				out = append(out, comp)
			}
		}
	}
	for v := 0; v < n; v++ {
		if index[v] == -1 && (g.Nodes[v].FileType == "" || g.Nodes[v].FileType == model.FileTypeCode) {
			strong(int32(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})
	return out
}

// Unreferenced is a code symbol that nothing else points at.
type Unreferenced struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	SourceFile string `json:"source_file,omitempty"`
	Degree     int    `json:"degree"`
}

// unreferencedSkip maps structural relations to true; incoming edges with
// those relations do not count as a reference.
var unreferencedSkip = map[string]bool{
	"contains": true, "method": true, "imports": true, "imports_from": true,
}

var entryPoints = map[string]bool{
	"main": true, "init": true, "__init__": true, "__main__": true,
	"setup": true, "run": true, "testmain": true,
}

// UnreferencedSymbols lists code symbols with no incoming non-structural edge:
// likely dead code or public API that is only entered from outside the corpus.
func UnreferencedSymbols(g *model.Graph, topN int) []Unreferenced {
	referenced := make([]bool, len(g.Nodes))
	for _, e := range g.Edges {
		if unreferencedSkip[e.Relation] {
			continue
		}
		if t := g.Index(e.Target); t >= 0 {
			referenced[t] = true
		}
	}
	out := make([]Unreferenced, 0, 32)
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.FileType != "" && n.FileType != model.FileTypeCode {
			continue
		}
		if model.IsFileExtLabel(n.Label) || n.SourceFile == "" || referenced[i] {
			continue
		}
		if entryPoints[strings.ToLower(strings.TrimSuffix(n.Label, "()"))] {
			continue
		}
		out = append(out, Unreferenced{ID: n.ID, Label: n.Label, SourceFile: n.SourceFile, Degree: g.Degree(int32(i))})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Degree != out[j].Degree {
			return out[i].Degree > out[j].Degree
		}
		return out[i].Label < out[j].Label
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}
