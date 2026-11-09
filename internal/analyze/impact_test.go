package analyze

import (
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func testGraph(ids ...string) *model.Graph {
	g := model.New()
	for _, id := range ids {
		g.AddNode(model.Node{ID: id, Label: id + "()", FileType: model.FileTypeCode, SourceFile: "main.go"})
	}
	return g
}

func TestImpactFollowsReverseCalls(t *testing.T) {
	g := testGraph("a", "b", "c")
	g.AddEdge(model.Edge{Source: "a", Target: "b", Relation: "calls", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "c", Relation: "calls", Confidence: model.ConfInferred})
	// structural edges never propagate impact
	g.AddEdge(model.Edge{Source: "c", Target: "b", Relation: "contains", Confidence: model.ConfExtracted})

	res := Impact(g, "c", 3)
	if !res.Found {
		t.Fatal("expected c to resolve")
	}
	if len(res.Dependents) != 2 {
		t.Fatalf("dependents = %d, want 2: %+v", len(res.Dependents), res.Dependents)
	}
	if res.Dependents[0].Label != "b()" || res.Dependents[0].Depth != 1 {
		t.Errorf("first dependent = %+v, want b() depth 1", res.Dependents[0])
	}
	if res.Dependents[1].Label != "a()" || res.Dependents[1].Depth != 2 {
		t.Errorf("second dependent = %+v, want a() depth 2", res.Dependents[1])
	}

	if Impact(g, "missing", 2).Found {
		t.Error("unexpected resolution for a missing node")
	}
}

func TestImpactRespectsHops(t *testing.T) {
	g := testGraph("a", "b", "c")
	g.AddEdge(model.Edge{Source: "a", Target: "b", Relation: "uses", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "c", Relation: "uses", Confidence: model.ConfInferred})

	if got := len(Impact(g, "c", 1).Dependents); got != 1 {
		t.Fatalf("hops=1 dependents = %d, want 1", got)
	}
	if got := len(Impact(g, "c", 2).Dependents); got != 2 {
		t.Fatalf("hops=2 dependents = %d, want 2", got)
	}
}
