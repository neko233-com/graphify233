package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func testGraph() *model.Graph {
	g := model.New()
	g.AddNode(model.Node{ID: "a", Label: "alpha()", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L1", Community: 0})
	g.AddNode(model.Node{ID: "b", Label: "beta()", FileType: model.FileTypeCode, SourceFile: "b.py", SourceLocation: "L2", Community: 1})
	g.AddEdge(model.Edge{Source: "a", Target: "b", Relation: "calls", Confidence: model.ConfInferred})
	g.BuildAdjacency()
	return g
}

func TestServeJSONRPC(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"graph_stats","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_node","arguments":{"name":"beta"}}}`,
	}, "\n") + "\n")

	if err := Serve(testGraph(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d responses, want 4 (notifications produce none):\n%s", len(lines), out.String())
	}
	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatalf("initialize response: %v", err)
	}
	if init.Result.ServerInfo.Name != "graphify233" || init.Result.ProtocolVersion == "" {
		t.Fatalf("bad initialize response: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"tools"`) {
		t.Fatalf("tools/list missing tools: %s", lines[1])
	}
	if !strings.Contains(lines[2], "2 nodes") {
		t.Fatalf("graph_stats wrong: %s", lines[2])
	}
	if !strings.Contains(lines[3], "beta()") {
		t.Fatalf("get_node wrong: %s", lines[3])
	}
}

func TestUnknownMethodIsError(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"bogus"}` + "\n")
	if err := Serve(testGraph(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if !strings.Contains(out.String(), `"error"`) {
		t.Fatalf("unknown method should return a JSON-RPC error: %s", out.String())
	}
}
