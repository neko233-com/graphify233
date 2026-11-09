package export

import (
	"path/filepath"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func smallGraph() *model.Graph {
	g := model.New()
	g.AddNode(model.Node{
		ID: "svc", Label: "service.py", FileType: model.FileTypeCode,
		SourceFile: "pkg/service.py", SourceLocation: "L1", Community: 2,
	})
	g.AddNode(model.Node{
		ID: "svc_run", Label: "run()", FileType: model.FileTypeCode,
		SourceFile: "pkg/service.py", SourceLocation: "L4", Community: 2,
	})
	g.AddNode(model.Node{
		ID: "ext", Label: "external", FileType: model.FileTypeDocument,
		SourceFile: "docs/ext.md", SourceLocation: "L1", Community: 0,
	})
	g.AddEdge(model.Edge{
		Source: "svc", Target: "svc_run", Relation: "contains",
		Confidence: model.ConfExtracted, SourceFile: "pkg/service.py",
		SourceLocation: "L4", Weight: 1, ConfidenceScore: 1,
	})
	g.AddEdge(model.Edge{
		Source: "svc_run", Target: "ext", Relation: "calls",
		Confidence: model.ConfInferred, SourceFile: "pkg/service.py",
		SourceLocation: "L5", Weight: 0.8, ConfidenceScore: 0.5,
	})
	return g
}

func TestJSONRoundTrip(t *testing.T) {
	g := smallGraph()
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := WriteJSON(path, g); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadJSON(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Nodes) != len(g.Nodes) {
		t.Fatalf("nodes = %d, want %d", len(loaded.Nodes), len(g.Nodes))
	}
	if len(loaded.Edges) != len(g.Edges) {
		t.Fatalf("edges = %d, want %d", len(loaded.Edges), len(g.Edges))
	}
	i := loaded.Index("svc")
	if i < 0 {
		t.Fatal("node svc missing after reload")
	}
	if loaded.Nodes[i].Community != 2 {
		t.Fatalf("community = %d, want 2", loaded.Nodes[i].Community)
	}
	var calls *model.Edge
	for _, e := range loaded.Edges {
		if e.Relation == "calls" {
			calls = e
		}
	}
	if calls == nil {
		t.Fatal("calls edge missing after reload")
	}
	if calls.Confidence != model.ConfInferred || calls.Weight != 0.8 {
		t.Fatalf("edge attrs lost: %+v", calls)
	}
}

func TestNodeLinkShape(t *testing.T) {
	link := buildNodeLink(smallGraph(), false)
	if link.Directed || link.Multigraph {
		t.Fatal("graph should be undirected and simple")
	}
	for _, n := range link.Nodes {
		if n.ID == "svc" && n.Community == nil {
			t.Fatal("assigned community encoded as null")
		}
	}
	for _, l := range link.Links {
		if l.Confidence == "" || l.ConfidenceScore == 0 {
			t.Fatalf("edge missing confidence metadata: %+v", l)
		}
	}
}
