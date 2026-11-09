package analyze

import (
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func TestCyclesFindsSCC(t *testing.T) {
	g := testGraph("a", "b", "c", "tail")
	g.AddEdge(model.Edge{Source: "a", Target: "b", Relation: "calls", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "c", Relation: "calls", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "c", Target: "a", Relation: "calls", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "c", Target: "tail", Relation: "calls", Confidence: model.ConfInferred})

	cycles := Cycles(g)
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly one", cycles)
	}
	if len(cycles[0]) != 3 {
		t.Fatalf("cycle members = %v, want a, b, c", cycles[0])
	}
	for _, id := range cycles[0] {
		if id == "tail" {
			t.Fatalf("tail must not be part of the cycle: %v", cycles[0])
		}
	}
}

func TestUnreferencedSkipsEntryPointsAndFileNodes(t *testing.T) {
	g := testGraph("main", "used", "dead")
	g.AddNode(model.Node{ID: "x_go", Label: "x.go", FileType: model.FileTypeCode, SourceFile: "x.go"})
	g.AddEdge(model.Edge{Source: "x_go", Target: "used", Relation: "contains", Confidence: model.ConfExtracted})
	g.AddEdge(model.Edge{Source: "main", Target: "used", Relation: "calls", Confidence: model.ConfInferred})

	got := UnreferencedSymbols(g, 10)
	labels := map[string]bool{}
	for _, u := range got {
		labels[u.Label] = true
	}
	if !labels["dead()"] {
		t.Errorf("dead() should be unreferenced, got %+v", got)
	}
	if labels["main()"] {
		t.Error("entry point main() must be skipped")
	}
	if labels["x.go"] {
		t.Error("file nodes must be skipped")
	}
}
