package cluster

import (
	"reflect"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

// twoCliques builds two fully connected groups joined by a single bridge edge.
func twoCliques(t *testing.T, size int) (*model.Graph, [][]int32) {
	t.Helper()
	g := model.New()
	add := func(id string) {
		g.AddNode(model.Node{ID: id, Label: id, FileType: model.FileTypeCode, Community: -1})
	}
	for _, prefix := range []string{"a", "b"} {
		for i := 0; i < size; i++ {
			add(prefix + string(rune('0'+i)))
		}
	}
	edge := func(s, tgt string) {
		if !g.AddEdge(model.Edge{Source: s, Target: tgt, Relation: "calls", Confidence: model.ConfInferred, Weight: 0.8}) {
			t.Fatalf("edge %s->%s rejected", s, tgt)
		}
	}
	for i := 0; i < size; i++ {
		for j := i + 1; j < size; j++ {
			edge("a"+string(rune('0'+i)), "a"+string(rune('0'+j)))
			edge("b"+string(rune('0'+i)), "b"+string(rune('0'+j)))
		}
	}
	edge("a0", "b0")
	return g, g.BuildAdjacency()
}

func TestDetectPartitionsEveryNode(t *testing.T) {
	g, adj := twoCliques(t, 5)
	comms := Detect(len(g.Nodes), adj)
	seen := make([]bool, len(g.Nodes))
	for _, c := range comms {
		if len(c) == 0 {
			t.Fatal("empty community")
		}
		for i := 1; i < len(c); i++ {
			if c[i-1] >= c[i] {
				t.Fatalf("members not ascending: %v", c)
			}
		}
		for _, m := range c {
			if seen[m] {
				t.Fatalf("node %d in more than one community", m)
			}
			seen[m] = true
		}
	}
	for i, ok := range seen {
		if !ok {
			t.Fatalf("node %d unassigned", i)
		}
	}
	if len(comms) != 2 {
		t.Fatalf("two cliques: got %d communities, want 2", len(comms))
	}
}

// fourCliques builds four dense groups joined in a ring. A multi-level Louvain
// that double-counts aggregated edge weights keeps merging them and collapses
// the partition, so this guards the aggregation step.
func fourCliques(t *testing.T, size int) (int, [][]int32) {
	t.Helper()
	g := model.New()
	groups := []string{"a", "b", "c", "d"}
	for _, prefix := range groups {
		for i := 0; i < size; i++ {
			g.AddNode(model.Node{ID: prefix + string(rune('0'+i)), Label: prefix + string(rune('0'+i)), Community: -1})
		}
	}
	for _, prefix := range groups {
		for i := 0; i < size; i++ {
			for j := i + 1; j < size; j++ {
				g.AddEdge(model.Edge{Source: prefix + string(rune('0'+i)), Target: prefix + string(rune('0'+j)), Relation: "calls"})
			}
		}
	}
	for i := 0; i < len(groups); i++ {
		a, b := groups[i], groups[(i+1)%len(groups)]
		g.AddEdge(model.Edge{Source: a + "0", Target: b + "0", Relation: "calls"})
	}
	return len(g.Nodes), g.BuildAdjacency()
}

func TestDetectMultiLevelKeepsGroups(t *testing.T) {
	n, adj := fourCliques(t, 6)
	comms := Detect(n, adj)
	if len(comms) != 4 {
		sizes := make([]int, len(comms))
		for i, c := range comms {
			sizes[i] = len(c)
		}
		t.Fatalf("four cliques: got %d communities %v, want 4", len(comms), sizes)
	}
	for _, c := range comms {
		if len(c) != 6 {
			t.Fatalf("community has %d nodes, want 6: %v", len(c), c)
		}
	}
}

func TestDetectDeterministic(t *testing.T) {
	g, adj := twoCliques(t, 6)
	first := Detect(len(g.Nodes), adj)
	second := Detect(len(g.Nodes), adj)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("community detection is not deterministic")
	}
}

func TestDetectIsolatesAndCohesion(t *testing.T) {
	g := model.New()
	g.AddNode(model.Node{ID: "solo", Label: "solo", FileType: model.FileTypeCode, Community: -1})
	g.AddNode(model.Node{ID: "x", Label: "x", FileType: model.FileTypeCode, Community: -1})
	g.AddEdge(model.Edge{Source: "solo", Target: "solo", Relation: "calls"})
	adj := g.BuildAdjacency()
	comms := Detect(len(g.Nodes), adj)
	if len(comms) != 2 {
		t.Fatalf("got %d communities, want 2: %v", len(comms), comms)
	}
	if len(comms[1]) != 1 || comms[1][0] != g.Index("x") {
		t.Fatalf("isolated node should be its own community: %v", comms)
	}
	c := Cohesion(comms[0], adj, make([]int32, len(g.Nodes)), 1)
	if c < 0 || c > 1 {
		t.Fatalf("cohesion out of range: %f", c)
	}
}

func TestStableOrderAnchorsCommunities(t *testing.T) {
	ids := []string{"zeta", "alpha", "mu", "beta"}
	deg := []int32{5, 5, 1, 2}
	comms := [][]int32{{2, 3}, {0, 1}}
	got := StableOrder(comms, ids, deg)
	if got[0][0] != 0 || got[1][0] != 2 {
		t.Fatalf("anchor order = %v, want communities {0,1} then {2,3}", got)
	}
	// Input order must not matter: the same partition passed in reverse
	// yields the same ids.
	rev := StableOrder([][]int32{{0, 1}, {2, 3}}, ids, deg)
	if !reflect.DeepEqual(got, rev) {
		t.Fatalf("order depends on input order: %v vs %v", got, rev)
	}
}

func TestStableOrderTieBreaksByID(t *testing.T) {
	ids := []string{"b", "a"}
	deg := []int32{2, 2}
	got := StableOrder([][]int32{{0}, {1}}, ids, deg)
	if got[0][0] != 1 {
		t.Fatalf("tie should put community anchored at %q first: %v", "a", got)
	}
}
