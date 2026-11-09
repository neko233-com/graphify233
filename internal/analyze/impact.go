package analyze

import (
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

// ImpactHit is one node that depends on a changed node.
type ImpactHit struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	FileType   string `json:"file_type,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
	Relation   string `json:"relation"`
	Depth      int    `json:"depth"`
	Degree     int    `json:"degree"`
}

// ImpactResult lists the reverse dependencies of a node.
type ImpactResult struct {
	Start      string      `json:"start"`
	StartLabel string      `json:"start_label,omitempty"`
	Found      bool        `json:"found"`
	Hops       int         `json:"hops"`
	Dependents []ImpactHit `json:"dependents"`
}

// impactSkip are structural relations: a file "contains" a symbol and a class
// has a "method"; those are not dependents that a change propagates to.
var impactSkip = map[string]bool{
	"contains": true, "method": true, "imports": true, "imports_from": true,
}

type revEdge struct {
	from int32
	rel  string
}

// Impact finds every node that transitively points at name through a
// non-structural relation (calls, uses, references, inherits, mentions, ...).
// It answers "what breaks if I change this?".
func Impact(g *model.Graph, name string, hops int) ImpactResult {
	if hops <= 0 {
		hops = 2
	}
	res := ImpactResult{Start: name, Hops: hops, Dependents: []ImpactHit{}}
	idx, ok := resolveNode(g, name)
	if !ok {
		return res
	}
	res.Found = true
	res.Start = g.Nodes[idx].ID
	res.StartLabel = g.Nodes[idx].Label

	rev := make([][]revEdge, len(g.Nodes))
	for _, e := range g.Edges {
		if impactSkip[e.Relation] {
			continue
		}
		s, t := g.Index(e.Source), g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		rev[t] = append(rev[t], revEdge{from: int32(s), rel: e.Relation})
	}

	depth := make([]int, len(g.Nodes))
	rel := make([]string, len(g.Nodes))
	for i := range depth {
		depth[i] = -1
	}
	depth[idx] = 0
	queue := []int32{idx}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if depth[u] >= hops {
			continue
		}
		for _, r := range rev[u] {
			if depth[r.from] >= 0 {
				continue
			}
			depth[r.from] = depth[u] + 1
			rel[r.from] = r.rel
			queue = append(queue, r.from)
		}
	}

	for i := range g.Nodes {
		if depth[i] <= 0 {
			continue
		}
		n := g.Nodes[i]
		res.Dependents = append(res.Dependents, ImpactHit{
			ID:         n.ID,
			Label:      n.Label,
			FileType:   n.FileType,
			SourceFile: n.SourceFile,
			Relation:   rel[i],
			Depth:      depth[i],
			Degree:     g.Degree(int32(i)),
		})
	}
	sort.SliceStable(res.Dependents, func(i, j int) bool {
		a, b := res.Dependents[i], res.Dependents[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		if a.Degree != b.Degree {
			return a.Degree > b.Degree
		}
		return a.Label < b.Label
	})
	return res
}

func resolveNode(g *model.Graph, name string) (int32, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, false
	}
	if i := g.Index(name); i >= 0 {
		return int32(i), true
	}
	lower := strings.ToLower(name)
	for i := range g.Nodes {
		if strings.ToLower(g.Nodes[i].Label) == lower {
			return int32(i), true
		}
	}
	for i := range g.Nodes {
		if strings.Contains(strings.ToLower(g.Nodes[i].Label), lower) {
			return int32(i), true
		}
	}
	return -1, false
}
