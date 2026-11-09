// Package graphbuild assembles per-file extractions into one graph.
package graphbuild

import (
	"github.com/neko233-com/graphify233/internal/model"
)

// FromExtractions merges extraction results into a graph the way the reference
// build step does: all nodes first, then edges (dangling edges are dropped),
// then isolated code nodes are removed.
func FromExtractions(exs []model.Extraction) *model.Graph {
	g := model.New()
	total := 0
	for _, ex := range exs {
		total += len(ex.Nodes)
	}
	g.Nodes = make([]*model.Node, 0, total)

	for i := range exs {
		for j := range exs[i].Nodes {
			g.AddNode(exs[i].Nodes[j])
		}
	}
	for i := range exs {
		for j := range exs[i].Edges {
			g.AddEdge(exs[i].Edges[j])
		}
		g.Hyperedges = append(g.Hyperedges, exs[i].Hyperedges...)
	}
	g.RemoveIsolatedCodeNodes()
	return g
}
