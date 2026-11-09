package analyze

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func diffGraph(nodes []model.Node, edges []model.Edge) *model.Graph {
	g := model.New()
	for _, n := range nodes {
		g.AddNode(n)
	}
	for _, e := range edges {
		g.AddEdge(e)
	}
	return g
}

func TestGraphDiffNodesAndEdges(t *testing.T) {
	oldG := diffGraph(
		[]model.Node{node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4")},
		[]model.Edge{edge("a", "b", "calls", model.ConfInferred, "a.py")},
	)
	newG := diffGraph(
		[]model.Node{node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4"), node("c", "gamma()", "b.py", "L2")},
		[]model.Edge{
			edge("a", "b", "calls", model.ConfInferred, "a.py"),
			edge("b", "c", "calls", model.ConfExtracted, "b.py"),
		},
	)
	d := GraphDiff(oldG, newG)
	if len(d.AddedNodes) != 1 || d.AddedNodes[0].ID != "c" {
		t.Fatalf("added nodes = %+v", d.AddedNodes)
	}
	if len(d.RemovedNodes) != 0 {
		t.Fatalf("removed nodes = %+v", d.RemovedNodes)
	}
	if len(d.AddedEdges) != 1 || d.AddedEdges[0].Source != "b" || d.AddedEdges[0].Target != "c" {
		t.Fatalf("added edges = %+v", d.AddedEdges)
	}
	if want := "nodes +1 -0 · edges +1 -0"; d.Summary() != want {
		t.Fatalf("summary = %q, want %q", d.Summary(), want)
	}
	var buf bytes.Buffer
	if err := d.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var got Diff
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("diff json: %v", err)
	}
	if len(got.AddedNodes) != 1 || len(got.AddedEdges) != 1 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestGraphDiffUndirectedEdgeIdentity(t *testing.T) {
	oldG := diffGraph(
		[]model.Node{node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4")},
		[]model.Edge{edge("a", "b", "calls", model.ConfInferred, "a.py")},
	)
	newG := diffGraph(
		[]model.Node{node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4")},
		[]model.Edge{edge("b", "a", "calls", model.ConfInferred, "a.py")},
	)
	if d := GraphDiff(oldG, newG); !d.Empty() {
		t.Fatalf("direction flip should not be a diff: %s", d.Summary())
	}
}

func TestGraphDiffCommunityRenumberingIsNotDrift(t *testing.T) {
	oldG := diffGraph(
		[]model.Node{
			node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4"),
			node("c", "gamma()", "b.py", "L1"),
		}, nil)
	oldG.Nodes[0].Community, oldG.Nodes[1].Community, oldG.Nodes[2].Community = 0, 0, 1
	newG := diffGraph(
		[]model.Node{
			node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4"),
			node("c", "gamma()", "b.py", "L1"),
		}, nil)
	newG.Nodes[0].Community, newG.Nodes[1].Community, newG.Nodes[2].Community = 1, 1, 0
	if d := GraphDiff(oldG, newG); len(d.MovedNodes) != 0 {
		t.Fatalf("renumbered communities reported as drift: %+v", d.MovedNodes)
	}
}

func TestGraphDiffReportsRealCommunityMove(t *testing.T) {
	oldG := diffGraph(
		[]model.Node{
			node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4"),
			node("c", "gamma()", "b.py", "L1"),
		}, nil)
	oldG.Nodes[0].Community, oldG.Nodes[1].Community, oldG.Nodes[2].Community = 0, 0, 1
	newG := diffGraph(
		[]model.Node{
			node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4"),
			node("c", "gamma()", "b.py", "L1"),
		}, nil)
	newG.Nodes[0].Community, newG.Nodes[1].Community, newG.Nodes[2].Community = 0, 1, 1
	d := GraphDiff(oldG, newG)
	if len(d.MovedNodes) != 1 || d.MovedNodes[0].ID != "b" {
		t.Fatalf("moved nodes = %+v", d.MovedNodes)
	}
}

func TestGraphDiffDetectsRemovals(t *testing.T) {
	full := diffGraph(
		[]model.Node{node("a", "alpha()", "a.py", "L1"), node("b", "beta()", "a.py", "L4")},
		[]model.Edge{edge("a", "b", "calls", model.ConfInferred, "a.py")},
	)
	shrunk := diffGraph([]model.Node{node("a", "alpha()", "a.py", "L1")}, nil)
	d := GraphDiff(full, shrunk)
	if len(d.RemovedNodes) != 1 || d.RemovedNodes[0].ID != "b" {
		t.Fatalf("removed nodes = %+v", d.RemovedNodes)
	}
	if len(d.RemovedEdges) != 1 {
		t.Fatalf("removed edges = %+v", d.RemovedEdges)
	}
}
