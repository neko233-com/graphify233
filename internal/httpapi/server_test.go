package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func newGraph() *model.Graph {
	g := model.New()
	g.AddNode(model.Node{ID: "alpha", Label: "alpha()", FileType: model.FileTypeCode, SourceFile: "a.go", Community: -1})
	g.AddNode(model.Node{ID: "beta", Label: "beta()", FileType: model.FileTypeCode, SourceFile: "b.go", Community: -1})
	g.AddNode(model.Node{ID: "gamma", Label: "gamma()", FileType: model.FileTypeCode, SourceFile: "c.go", Community: -1})
	g.AddEdge(model.Edge{Source: "alpha", Target: "beta", Relation: "calls", Confidence: model.ConfInferred, Weight: 0.8, ConfidenceScore: 0.8})
	g.AddEdge(model.Edge{Source: "alpha", Target: "gamma", Relation: "calls", Confidence: model.ConfInferred, Weight: 0.8, ConfidenceScore: 0.8})
	return g
}

func request(t *testing.T, srv *httptest.Server, method, path, body, token string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestHandlerTokenAndEndpoints(t *testing.T) {
	srv := httptest.NewServer(Handler(newGraph(), "s3cret"))
	defer srv.Close()

	if code, _ := request(t, srv, "GET", "/healthz", "", ""); code != http.StatusOK {
		t.Errorf("healthz without token = %d, want 200", code)
	}
	if code, _ := request(t, srv, "GET", "/stats", "", ""); code != http.StatusUnauthorized {
		t.Errorf("stats without token = %d, want 401", code)
	}
	code, body := request(t, srv, "GET", "/stats", "", "s3cret")
	if code != http.StatusOK || !strings.Contains(body, `"nodes": 3`) {
		t.Errorf("stats = %d %s", code, body)
	}
	code, body = request(t, srv, "GET", "/query?q=alpha", "", "s3cret")
	if code != http.StatusOK || !strings.Contains(body, "alpha") {
		t.Errorf("query = %d %s", code, body)
	}
	code, body = request(t, srv, "POST", "/query", `{"question":"beta","depth":1}`, "s3cret")
	if code != http.StatusOK || !strings.Contains(body, "seeds") {
		t.Errorf("query POST = %d %s", code, body)
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
	code, body = request(t, srv, "POST", "/mcp", init, "s3cret")
	if code != http.StatusOK || !strings.Contains(body, "serverInfo") {
		t.Errorf("mcp = %d %s", code, body)
	}
	note := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	if code, _ := request(t, srv, "POST", "/mcp", note, "s3cret"); code != http.StatusAccepted {
		t.Errorf("notification status = %d, want 202", code)
	}
}

func TestHandlerWithoutToken(t *testing.T) {
	srv := httptest.NewServer(Handler(newGraph(), ""))
	defer srv.Close()
	if code, body := request(t, srv, "GET", "/god?top=5", "", ""); code != http.StatusOK || !strings.Contains(body, "alpha") {
		t.Errorf("god = %d %s", code, body)
	}
}
