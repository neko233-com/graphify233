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

// CommunityMove records a node whose community assignment changed.
type CommunityMove struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	From  int    `json:"from"`
	To    int    `json:"to"`
}

// Diff describes how a graph changed between two snapshots.
type Diff struct {
	AddedNodes   []DiffNode      `json:"added_nodes"`
	RemovedNodes []DiffNode      `json:"removed_nodes"`
	AddedEdges   []DiffEdge      `json:"added_edges"`
	RemovedEdges []DiffEdge      `json:"removed_edges"`
	MovedNodes   []CommunityMove `json:"moved_nodes,omitempty"`
}

// Empty reports whether the two snapshots are structurally identical.
func (d Diff) Empty() bool {
	return len(d.AddedNodes) == 0 && len(d.RemovedNodes) == 0 &&
		len(d.AddedEdges) == 0 && len(d.RemovedEdges) == 0 && len(d.MovedNodes) == 0
}

// Summary is a one-line description of the change.
func (d Diff) Summary() string {
	if d.Empty() {
		return "no structural changes"
	}
	s := fmt.Sprintf("nodes +%d -%d · edges +%d -%d",
		len(d.AddedNodes), len(d.RemovedNodes), len(d.AddedEdges), len(d.RemovedEdges))
	if len(d.MovedNodes) > 0 {
		s += fmt.Sprintf(" · communities moved %d", len(d.MovedNodes))
	}
	return s
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

// matchCommunities maps each old community id to the new community that shares
// the most surviving nodes with it. Community ids are renumbered between runs
// (sorted by size), so a raw id comparison would report almost every node as
// having moved; membership overlap is what "drift" actually means.
func matchCommunities(oldG, newG *model.Graph) map[int]int {
	oldByID := make(map[string]*model.Node, len(oldG.Nodes))
	for _, n := range oldG.Nodes {
		oldByID[n.ID] = n
	}
	type pair struct{ old, new, overlap int }
	counts := make(map[[2]int]int)
	for _, n := range newG.Nodes {
		if n.Community < 0 {
			continue
		}
		oldN, ok := oldByID[n.ID]
		if !ok || oldN.Community < 0 {
			continue
		}
		counts[[2]int{oldN.Community, n.Community}]++
	}
	pairs := make([]pair, 0, len(counts))
	for k, c := range counts {
		pairs = append(pairs, pair{old: k[0], new: k[1], overlap: c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].overlap != pairs[j].overlap {
			return pairs[i].overlap > pairs[j].overlap
		}
		if pairs[i].old != pairs[j].old {
			return pairs[i].old < pairs[j].old
		}
		return pairs[i].new < pairs[j].new
	})
	mapping := make(map[int]int, len(pairs))
	usedNew := make(map[int]bool, len(pairs))
	for _, p := range pairs {
		if _, ok := mapping[p.old]; ok {
			continue
		}
		if usedNew[p.new] {
			continue
		}
		mapping[p.old] = p.new
		usedNew[p.new] = true
	}
	return mapping
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
	matched := matchCommunities(oldG, newG)
	for _, n := range oldG.Nodes {
		cur, ok := newNodes[n.ID]
		if !ok {
			d.RemovedNodes = append(d.RemovedNodes, nodeRef(n))
			continue
		}
		// Community drift: the node survived but the clustering moved it. The
		// old community is first mapped to its best-overlap counterpart so
		// per-run renumbering alone is not reported as a move.
		if n.Community >= 0 && cur.Community >= 0 {
			if want, ok := matched[n.Community]; ok && cur.Community != want {
				d.MovedNodes = append(d.MovedNodes, CommunityMove{
					ID: n.ID, Label: cur.Label, From: n.Community, To: cur.Community,
				})
			}
		}
	}
	sort.Slice(d.MovedNodes, func(i, j int) bool { return d.MovedNodes[i].ID < d.MovedNodes[j].ID })

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
