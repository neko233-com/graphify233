package enrich

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func testGraph() *model.Graph {
	g := model.New()
	add := func(n model.Node) {
		n.Community = -1
		g.AddNode(n)
	}
	add(model.Node{ID: "readme", Label: "README.md", FileType: model.FileTypeDocument, SourceFile: "README.md", SourceLocation: "L1"})
	add(model.Node{ID: "run", Label: "run()", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L2"})
	add(model.Node{ID: "graph", Label: "Graph", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L5"})
	add(model.Node{ID: "dup1", Label: "dup()", FileType: model.FileTypeCode, SourceFile: "a.py"})
	add(model.Node{ID: "dup2", Label: "dup()", FileType: model.FileTypeCode, SourceFile: "b.py"})
	return g
}

func TestMentionsLinksUniqueSymbols(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "README.md")
	if err := os.WriteFile(doc, []byte("# Docs\n\nCall run() and then Graph.\nThe dup symbol is ambiguous.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := testGraph()
	n := Mentions(g, []Doc{{Path: doc, Rel: "README.md", Kind: model.FileTypeDocument}}, Limits{})
	if n != 2 {
		t.Fatalf("mentions = %d, want 2 (ambiguous dup must be skipped)", n)
	}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if e.Relation != "mentions" {
			continue
		}
		seen[e.Target] = true
		if e.Source != "readme" {
			t.Fatalf("mention source = %q, want readme", e.Source)
		}
		if e.Confidence != model.ConfInferred {
			t.Fatalf("confidence = %q, want INFERRED", e.Confidence)
		}
		if e.SourceLocation != "L3" {
			t.Fatalf("location = %q, want L3", e.SourceLocation)
		}
	}
	if !seen["run"] || !seen["graph"] {
		t.Fatalf("missing mention targets: %v", seen)
	}
	if seen["dup1"] || seen["dup2"] {
		t.Fatalf("ambiguous label produced an edge: %v", seen)
	}
}

func TestMentionsSkipsExistingEdgesAndCaps(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "README.md")
	if err := os.WriteFile(doc, []byte("run() run() run()\nGraph\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := testGraph()
	g.AddEdge(model.Edge{Source: "readme", Target: "run", Relation: "references", Confidence: model.ConfExtracted})
	n := Mentions(g, []Doc{{Path: doc, Rel: "README.md", Kind: model.FileTypeDocument}}, Limits{})
	if n != 1 {
		t.Fatalf("mentions = %d, want 1 (existing edge skipped, repeated token deduped)", n)
	}
	for _, e := range g.Edges {
		if e.Relation == "mentions" && e.Target != "graph" {
			t.Fatalf("unexpected mention to %q", e.Target)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"run()":      "run",
		".start()":   "start",
		"Graph":      "graph",
		"__init__":   "__init__",
		"HTTPClient": "httpclient",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
