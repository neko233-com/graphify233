package enrich

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func TestConceptsFromHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.md")
	text := "# Authentication\n\nThe system issues tokens.\n\n## Tokens\n\nCall authenticate() to start a session.\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	g := model.New()
	g.AddNode(model.Node{ID: "docs_auth_md", Label: "auth.md", FileType: model.FileTypeDocument, SourceFile: "docs/auth.md", Community: -1})
	g.AddNode(model.Node{ID: "auth_authenticate", Label: "authenticate()", FileType: model.FileTypeCode, SourceFile: "auth.go", Community: -1})

	concepts, mentions := Concepts(g, []Doc{{Path: path, Rel: "docs/auth.md", Kind: model.FileTypeDocument}}, Limits{})
	if concepts != 2 {
		t.Fatalf("concepts = %d, want 2", concepts)
	}
	if mentions != 1 {
		t.Fatalf("mentions = %d, want 1", mentions)
	}

	wantID := "auth_authentication"
	if g.Index(wantID) < 0 {
		t.Fatalf("concept node %q missing; nodes = %d", wantID, len(g.Nodes))
	}
	concept := g.Nodes[g.Index(wantID)]
	if concept.Label != "Authentication" || concept.FileType != model.FileTypeDocument || concept.SourceLocation != "L1" {
		t.Errorf("concept = %+v", concept)
	}
	if !g.HasEdge("docs_auth_md", wantID) {
		t.Error("doc should contain the concept")
	}
	if !g.HasEdge(wantID, "auth_authenticate") && !g.HasEdge("auth_tokens", "auth_authenticate") {
		t.Error("concept should mention authenticate()")
	}
}

func TestConceptsSkipsFencedHeadings(t *testing.T) {
	headings := parseHeadings([]string{"# Real", "```", "# Not a heading", "```", "## Also real"}, 10)
	if len(headings) != 2 || headings[0].title != "Real" || headings[1].title != "Also real" {
		t.Fatalf("headings = %+v", headings)
	}
}
