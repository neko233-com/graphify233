package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/neko233-com/graphify233/internal/model"
)

// DiffNode is a lightweight node reference in a graph diff.
type DiffNode struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	FileType   string `json:"file_type,omitempty"`
	SourceFile string `json:"source_file,omitempty"`
}

// DiffEdge is a lightweight edge reference in a graph diff. Edges are
// undirected, so Source and Target are canonicalised (Source <= Target).
type DiffEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Confidence string `json:"confidence,omitempty"`
}

// Diff describes how a graph changed between two snapshots.
type Diff struct {
	AddedNodes   []DiffNode `json:"added_nodes"`
	RemovedNodes []DiffNode `json:"removed_nodes"`
	AddedEdges   []DiffEdge `json:"added_edges"`
	RemovedEdges []DiffEdge `json:"removed_edges"`
}

// Empty reports whether the two snapshots are structurally identical.
func (d Diff) Empty() bool {
	return len(d.AddedNodes) == 0 && len(d.RemovedNodes) == 0 &&
		len(d.AddedEdges) == 0 && len(d.RemovedEdges) == 0
}

// Summary is a one-line description of the change.
func (d Diff) Summary() string {
	if d.Empty() {
		return "no structural changes"
	}
	return fmt.Sprintf("nodes +%d -%d · edges +%d -%d",
		len(d.AddedNodes), len(d.RemovedNodes), len(d.AddedEdges), len(d.RemovedEdges))
}

// WriteJSON encodes the diff as indented JSON.
func (d Diff) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

func nodeRef(n *model.Node) DiffNode {
	return DiffNode{ID: n.ID, Label: n.Label, FileType: n.FileType, SourceFile: n.SourceFile}
}

func edgeKey(source, target, relation string) string {
	if source > target {
		source, target = target, source
	}
	return source + "\x00" + target + "\x00" + relation
}

func edgeRef(e *model.Edge) DiffEdge {
	s, t := e.Source, e.Target
	if s > t {
		s, t = t, s
	}
	return DiffEdge{Source: s, Target: t, Relation: e.Relation, Confidence: e.Confidence}
}

// GraphDiff compares two graphs by node ID and by (endpoint, endpoint,
// relation). Attribute-only changes are ignored; the result is deterministic.
func GraphDiff(oldG, newG *model.Graph) Diff {
	d := Diff{
		AddedNodes:   []DiffNode{},
		RemovedNodes: []DiffNode{},
		AddedEdges:   []DiffEdge{},
		RemovedEdges: []DiffEdge{},
	}
	oldNodes := make(map[string]*model.Node, len(oldG.Nodes))
	for _, n := range oldG.Nodes {
		oldNodes[n.ID] = n
	}
	newNodes := make(map[string]*model.Node, len(newG.Nodes))
	for _, n := range newG.Nodes {
		newNodes[n.ID] = n
		if _, ok := oldNodes[n.ID]; !ok {
			d.AddedNodes = append(d.AddedNodes, nodeRef(n))
		}
	}
	for _, n := range oldG.Nodes {
		if _, ok := newNodes[n.ID]; !ok {
			d.RemovedNodes = append(d.RemovedNodes, nodeRef(n))
		}
	}

	oldEdges := make(map[string]*model.Edge, len(oldG.Edges))
	for _, e := range oldG.Edges {
		oldEdges[edgeKey(e.Source, e.Target, e.Relation)] = e
	}
	newEdges := make(map[string]*model.Edge, len(newG.Edges))
	for _, e := range newG.Edges {
		key := edgeKey(e.Source, e.Target, e.Relation)
		newEdges[key] = e
		if _, ok := oldEdges[key]; !ok {
			d.AddedEdges = append(d.AddedEdges, edgeRef(e))
		}
	}
	for key, e := range oldEdges {
		if _, ok := newEdges[key]; !ok {
			d.RemovedEdges = append(d.RemovedEdges, edgeRef(e))
		}
	}

	sort.Slice(d.AddedNodes, func(i, j int) bool { return d.AddedNodes[i].ID < d.AddedNodes[j].ID })
	sort.Slice(d.RemovedNodes, func(i, j int) bool { return d.RemovedNodes[i].ID < d.RemovedNodes[j].ID })
	edgeLess := func(a, b DiffEdge) bool {
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		return a.Relation < b.Relation
	}
	sort.Slice(d.AddedEdges, func(i, j int) bool { return edgeLess(d.AddedEdges[i], d.AddedEdges[j]) })
	sort.Slice(d.RemovedEdges, func(i, j int) bool { return edgeLess(d.RemovedEdges[i], d.RemovedEdges[j]) })
	return d
}
