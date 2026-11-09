package query

import (
	"strings"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func chainGraph() *model.Graph {
	g := model.New()
	for _, n := range []model.Node{
		{ID: "a", Label: "alpha()", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L1", Community: 0},
		{ID: "b", Label: "beta()", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L4", Community: 0},
		{ID: "c", Label: "gamma()", FileType: model.FileTypeCode, SourceFile: "b.py", SourceLocation: "L2", Community: 1},
	} {
		g.AddNode(n)
	}
	g.AddEdge(model.Edge{Source: "a", Target: "b", Relation: "calls", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "c", Relation: "calls", Confidence: model.ConfInferred})
	g.BuildAdjacency()
	return g
}

func TestTermsDropStopwords(t *testing.T) {
	terms := Terms("How does the cache work in this project?")
	joined := strings.Join(terms, ",")
	for _, want := range []string{"cache", "project"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing term %q in %v", want, terms)
		}
	}
	for _, bad := range []string{"the", "how", "does"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("stopword %q kept: %v", bad, terms)
		}
	}
}

func TestResolveAndExplain(t *testing.T) {
	e := New(chainGraph())
	if _, ok := e.Resolve("gamma()"); !ok {
		t.Fatal("resolve by label failed")
	}
	text, err := e.Explain("gamma")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(text, "gamma()") || !strings.Contains(text, "b.py") {
		t.Fatalf("explain output missing details:\n%s", text)
	}
}

func TestPathAndQuery(t *testing.T) {
	e := New(chainGraph())
	p, err := e.Path("alpha", "gamma")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if len(p) != 3 || p[0] != "alpha()" || p[2] != "gamma()" {
		t.Fatalf("path = %v", p)
	}
	text, ids := e.Query("gamma", 2, 500)
	if len(ids) == 0 {
		t.Fatal("query matched nothing")
	}
	if !strings.Contains(text, "NODE gamma()") || !strings.Contains(text, "EDGE beta() --calls--> gamma()") {
		t.Fatalf("query subgraph incomplete:\n%s", text)
	}
}

func docGraph() *model.Graph {
	g := model.New()
	for _, n := range []model.Node{
		{ID: "docs_auth_md", Label: "AUTH.md", FileType: model.FileTypeDocument, SourceFile: "docs/AUTH.md", SourceLocation: "L1", Community: 0},
		{ID: "authenticate", Label: "authenticate()", FileType: model.FileTypeCode, SourceFile: "auth.go", SourceLocation: "L10", Community: 0},
		{ID: "readme_md", Label: "README.md", FileType: model.FileTypeDocument, SourceFile: "README.md", SourceLocation: "L1", Community: 0},
	} {
		g.AddNode(n)
	}
	g.AddEdge(model.Edge{Source: "docs_auth_md", Target: "authenticate", Relation: "mentions", Confidence: model.ConfInferred, Weight: 0.6})
	g.BuildAdjacency()
	return g
}

func TestQueryFallsBackToDocumentMentions(t *testing.T) {
	e := New(docGraph())
	text, ids := e.Query("how does auth work", 2, 600)
	if len(ids) != 1 || ids[0] != "authenticate" {
		t.Fatalf("ids = %v, want [authenticate]", ids)
	}
	if !strings.Contains(text, "NODE authenticate()") {
		t.Fatalf("subgraph missing mentioned symbol:\n%s", text)
	}
	if text, ids := e.Query("zzz", 2, 600); len(ids) != 0 || !strings.Contains(text, "No matching") {
		t.Fatalf("unexpected fallback for unknown terms: %q %v", text, ids)
	}
}
