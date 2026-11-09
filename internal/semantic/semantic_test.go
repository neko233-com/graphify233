package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func node(id, label, ft string) model.Node {
	return model.Node{ID: id, Label: label, FileType: ft, SourceFile: "x.go", Community: -1}
}

func TestConceptsLinksOverlappingSections(t *testing.T) {
	g := model.New()
	g.AddNode(node("a", "Auth", model.FileTypeDocument))
	g.AddNode(node("b", "Tokens", model.FileTypeDocument))
	g.AddNode(node("s1", "issue()", model.FileTypeCode))
	g.AddNode(node("s2", "verify()", model.FileTypeCode))
	g.AddNode(node("s3", "expire()", model.FileTypeCode))
	g.AddEdge(model.Edge{Source: "a", Target: "s1", Relation: "mentions", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "a", Target: "s2", Relation: "mentions", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "s2", Relation: "mentions", Confidence: model.ConfInferred})

	if n := Concepts(g, 0.5, Limits{}); n != 1 {
		t.Fatalf("edges = %d, want 1", n)
	}
	if !g.HasEdge("a", "b") {
		t.Fatal("expected a-b similarity edge")
	}
	found := false
	for _, e := range g.Edges {
		if e.Relation == "semantically_similar_to" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a semantically_similar_to edge")
	}
}

func TestConceptsRespectsThreshold(t *testing.T) {
	g := model.New()
	g.AddNode(node("a", "Auth", model.FileTypeDocument))
	g.AddNode(node("b", "Tokens", model.FileTypeDocument))
	g.AddNode(node("s1", "issue()", model.FileTypeCode))
	g.AddNode(node("s2", "verify()", model.FileTypeCode))
	g.AddNode(node("s3", "expire()", model.FileTypeCode))
	g.AddEdge(model.Edge{Source: "a", Target: "s1", Relation: "mentions", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "a", Target: "s2", Relation: "mentions", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "s2", Relation: "mentions", Confidence: model.ConfInferred})
	g.AddEdge(model.Edge{Source: "b", Target: "s3", Relation: "mentions", Confidence: model.ConfInferred})

	// Jaccard is 1/3 here, below the 0.8 threshold.
	if n := Concepts(g, 0.8, Limits{}); n != 0 {
		t.Fatalf("edges = %d, want 0", n)
	}
}

func TestEmbeddingLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q", req.Model)
		}
		vecs := map[string][]float64{"alpha": {1, 0}, "beta": {0.99, 0.01}, "gamma": {0, 1}}
		type item struct {
			Embedding []float64 `json:"embedding"`
		}
		out := struct {
			Data []item `json:"data"`
		}{}
		for _, text := range req.Input {
			v, ok := vecs[text]
			if !ok {
				v = []float64{0, 0}
			}
			out.Data = append(out.Data, item{Embedding: v})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	g := model.New()
	g.AddNode(node("alpha", "alpha()", model.FileTypeCode))
	g.AddNode(node("beta", "beta()", model.FileTypeCode))
	g.AddNode(node("gamma", "gamma()", model.FileTypeCode))

	client := &EmbeddingClient{URL: srv.URL, Model: "test-model"}
	n, err := EmbeddingLinks(context.Background(), g, client, 0.9, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("edges = %d, want 1 (alpha-beta)", n)
	}
	if !g.HasEdge("alpha", "beta") {
		t.Fatal("expected alpha-beta edge")
	}
	if g.HasEdge("alpha", "gamma") {
		t.Fatal("gamma is orthogonal, no edge expected")
	}
}

func TestEmbeddingClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	client := &EmbeddingClient{URL: srv.URL}
	if _, err := client.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestHashingEmbedderLinksRelatedIdentifiers(t *testing.T) {
	g := model.New()
	for _, n := range []model.Node{
		{ID: "login_user", Label: "login_user()", FileType: model.FileTypeCode, SourceFile: "auth.go", Community: -1},
		{ID: "user_login", Label: "user_login()", FileType: model.FileTypeCode, SourceFile: "auth.go", Community: -1},
		{ID: "render_page", Label: "render_page()", FileType: model.FileTypeCode, SourceFile: "web.go", Community: -1},
	} {
		g.AddNode(n)
	}
	n, err := EmbeddingLinks(context.Background(), g, HashingEmbedder{Dim: 256}, 0.8, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected at least one similarity edge")
	}
	if !g.HasEdge("login_user", "user_login") {
		t.Fatal("related identifiers should be linked")
	}
	if g.HasEdge("render_page", "login_user") {
		t.Fatal("unrelated identifiers should not be linked")
	}
}
