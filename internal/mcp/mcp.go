// Package mcp exposes the graph over the Model Context Protocol stdio
// transport, so assistants can query the knowledge graph directly.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/query"
)

// Version is reported in the MCP handshake.
const Version = "1.0.0"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Serve runs the JSON-RPC loop until EOF.
func Serve(g *model.Graph, in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 1<<20)
	w := bufio.NewWriterSize(out, 1<<20)
	defer w.Flush()
	for {
		line, err := r.ReadString('\n')
		if len(strings.TrimSpace(line)) > 0 {
			if enc := HandleBytes(g, []byte(line)); enc != nil {
				w.Write(enc)
				w.WriteByte('\n')
				if ferr := w.Flush(); ferr != nil {
					return ferr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// HandleBytes processes one JSON-RPC message and returns its encoded response,
// or nil when the message is a notification. It is the transport-neutral entry
// point used by both the stdio loop and HTTP.
func HandleBytes(g *model.Graph, raw []byte) []byte {
	resp := handle(query.New(g), raw)
	if resp == nil {
		return nil
	}
	enc, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	return enc
}

func handle(eng *query.Engine, raw []byte) *response {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}}
	}
	reply := func(result any) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}
	switch req.Method {
	case "initialize":
		return reply(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "graphify233", "version": Version},
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{"tools": tools()})
	case "tools/call":
		var call toolCall
		if err := json.Unmarshal(req.Params, &call); err != nil {
			return fail(-32602, "invalid params: "+err.Error())
		}
		text, err := callTool(eng, call)
		if err != nil {
			return reply(map[string]any{
				"isError": true,
				"content": []map[string]string{{"type": "text", "text": err.Error()}},
			})
		}
		return reply(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}

func tools() []tool {
	node := func(desc string, extra map[string]any) map[string]any {
		props := map[string]any{"name": map[string]any{"type": "string", "description": desc}}
		for k, v := range extra {
			props[k] = v
		}
		return map[string]any{"type": "object", "properties": props, "required": []string{"name"}}
	}
	depth := map[string]any{"type": "integer", "description": "traversal depth (default 2)", "minimum": 1, "maximum": 6}
	return []tool{
		{Name: "query_graph", Description: "Search the knowledge graph and return the most relevant subgraph.", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "natural-language question"},
				"depth":    depth,
			},
			"required": []string{"question"},
		}},
		{Name: "get_node", Description: "Explain one node: source location, degree and all connections by relation.", InputSchema: node("node label or id", nil)},
		{Name: "get_neighbors", Description: "Return the subgraph around a node.", InputSchema: node("node label or id", map[string]any{"depth": depth})},
		{Name: "get_community", Description: "List the members of the community containing a node.", InputSchema: node("node label or id", nil)},
		{Name: "god_nodes", Description: "List the most connected nodes (core abstractions).", InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"top": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}},
		}},
		{Name: "graph_stats", Description: "Return graph size and composition.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
		{Name: "shortest_path", Description: "Shortest path between two nodes.", InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from": map[string]any{"type": "string"},
				"to":   map[string]any{"type": "string"},
			},
			"required": []string{"from", "to"},
		}},
	}
}

func callTool(eng *query.Engine, call toolCall) (string, error) {
	str := func(k string) string {
		if v, ok := call.Arguments[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	num := func(k string, def int) int {
		if v, ok := call.Arguments[k]; ok {
			if f, ok := v.(float64); ok {
				return int(f)
			}
		}
		return def
	}
	switch call.Name {
	case "query_graph":
		q := str("question")
		if q == "" {
			return "", fmt.Errorf("question is required")
		}
		text, _ := eng.Query(q, num("depth", 3), 2000)
		return text, nil
	case "get_node":
		return eng.Explain(str("name"))
	case "get_neighbors":
		return eng.Neighbors(str("name"), num("depth", 2))
	case "get_community":
		name, members, err := eng.Community(str("name"))
		if err != nil {
			return "", err
		}
		return name + ":\n- " + strings.Join(members, "\n- "), nil
	case "god_nodes":
		return strings.Join(eng.GodNodes(num("top", 10)), "\n"), nil
	case "graph_stats":
		return eng.Stats(), nil
	case "shortest_path":
		path, err := eng.Path(str("from"), str("to"))
		if err != nil {
			return "", err
		}
		return strings.Join(path, " -> "), nil
	}
	return "", fmt.Errorf("unknown tool: %s", call.Name)
}
