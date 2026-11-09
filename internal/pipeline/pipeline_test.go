package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neko233-com/graphify233/internal/export"
)

const samplePython = `"""Widget helpers."""

import os


class Widget:
    def __init__(self, name):
        self.name = name

    def render(self):
        return self.draw() + os.sep

    def draw(self):
        return "x"
`

func writeCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "widget.py"), []byte(samplePython), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Widget\n\nThe widget renders text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=do-not-read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunWritesArtifactsAndCaches(t *testing.T) {
	root := writeCorpus(t)
	formats := DefaultFormats()
	formats.Wiki = true
	formats.SVG = true
	formats.GraphML = true
	formats.Cypher = true
	formats.Canvas = true

	res, err := Run(Options{Root: root, Formats: formats})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Graph == nil || len(res.Graph.Nodes) < 4 {
		t.Fatalf("graph too small: %d nodes", len(res.Graph.Nodes))
	}
	if res.FilesExtract != 2 {
		t.Fatalf("extracted %d files, want 2", res.FilesExtract)
	}
	if res.FilesFailed != 0 {
		t.Fatalf("%d files failed", res.FilesFailed)
	}
	for _, name := range []string{
		"graph.json", "GRAPH_REPORT.md", "graph.html", "graph.svg",
		"graph.graphml", "graph.cypher", "graph.canvas",
	} {
		if _, err := os.Stat(filepath.Join(root, "graphify-out", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "graphify-out", "obsidian")); err != nil {
		t.Fatalf("missing obsidian output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "graphify-out", "wiki", "index.md")); err != nil {
		t.Fatalf("missing wiki index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "graphify-out", "manifest.json")); err != nil {
		t.Fatalf("missing manifest: %v", err)
	}

	// Sensitive files must never be parsed into the graph.
	loaded, err := export.LoadJSON(filepath.Join(root, "graphify-out", "graph.json"))
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	for _, n := range loaded.Nodes {
		if filepath.Base(n.SourceFile) == ".env" {
			t.Fatalf("sensitive file leaked into graph: %+v", n)
		}
	}

	// A second run with --update must reuse the cache entirely.
	again, err := Run(Options{Root: root, Formats: formats, Update: true})
	if err != nil {
		t.Fatalf("update run: %v", err)
	}
	if again.FilesExtract != 0 || again.FilesCached != 2 {
		t.Fatalf("cache not reused: extracted=%d cached=%d", again.FilesExtract, again.FilesCached)
	}
	if len(again.Graph.Nodes) != len(res.Graph.Nodes) {
		t.Fatalf("incremental rebuild changed node count: %d -> %d", len(res.Graph.Nodes), len(again.Graph.Nodes))
	}
	if len(again.Communities) != len(res.Communities) {
		t.Fatalf("incremental rebuild changed communities: %d -> %d", len(res.Communities), len(again.Communities))
	}
}

func TestRunRejectsEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Run(Options{Root: root, Formats: DefaultFormats()}); err == nil {
		t.Fatal("expected an error for an empty corpus")
	}
}

const deepPython = `def widget_run():
    return 1
`

func TestRunDeepLinksDocumentationToCode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "widget.py"), []byte(deepPython), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide\n\nCall widget_run before rendering.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	standard, err := Run(Options{Root: root, Formats: DefaultFormats()})
	if err != nil {
		t.Fatal(err)
	}
	if standard.Mentions != 0 {
		t.Fatalf("standard mode linked %d mentions, want 0", standard.Mentions)
	}

	deep, err := Run(Options{Root: root, Formats: DefaultFormats(), Mode: "deep"})
	if err != nil {
		t.Fatal(err)
	}
	if deep.Mentions < 1 {
		t.Fatalf("deep mode linked %d mentions, want >= 1", deep.Mentions)
	}
	found := false
	for _, e := range deep.Graph.Edges {
		if e.Relation != "mentions" {
			continue
		}
		if tgt := deep.Graph.Nodes[deep.Graph.Index(e.Target)]; tgt.Label == "widget_run()" {
			found = true
			if e.SourceLocation != "L3" {
				t.Fatalf("mention location = %q, want L3", e.SourceLocation)
			}
		}
	}
	if !found {
		t.Fatal("guide.md did not mention widget_run()")
	}
}
