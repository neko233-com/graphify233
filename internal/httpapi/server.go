// Package httpapi exposes a built graph over HTTP: JSON endpoints for agents
// and scripts, plus a streamable-HTTP MCP endpoint.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/neko233-com/graphify233/internal/mcp"
	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/query"
)

// Handler builds the HTTP API. When token is non-empty every endpoint except
// /healthz requires "Authorization: Bearer <token>".
func Handler(g *model.Graph, token string) http.Handler {
	eng := query.New(g)
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"nodes": len(g.Nodes), "edges": len(g.Edges), "stats": eng.Stats(),
		})
	})
	mux.HandleFunc("/god", func(w http.ResponseWriter, r *http.Request) {
		top := intParam(r, "top", 10)
		writeJSON(w, map[string]any{"nodes": eng.GodNodes(top)})
	})
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		depth := intParam(r, "depth", 3)
		budget := intParam(r, "budget", 2000)
		if r.Method == http.MethodPost {
			var body struct {
				Question string `json:"question"`
				Query    string `json:"q"`
				Depth    int    `json:"depth"`
				Budget   int    `json:"budget"`
			}
			defer r.Body.Close()
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
				return
			}
			if body.Question != "" {
				q = body.Question
			} else if body.Query != "" {
				q = body.Query
			}
			if body.Depth > 0 {
				depth = body.Depth
			}
			if body.Budget > 0 {
				budget = body.Budget
			}
		}
		if strings.TrimSpace(q) == "" {
			http.Error(w, "question is required (q=...)", http.StatusBadRequest)
			return
		}
		writeJSON(w, eng.QueryJSON(q, depth, budget))
	})
	mux.HandleFunc("/node", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Explain(r.URL.Query().Get("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"text": out})
	})
	mux.HandleFunc("/neighbors", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Neighbors(r.URL.Query().Get("name"), intParam(r, "depth", 2))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"text": out})
	})
	mux.HandleFunc("/path", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Path(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"path": out})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST with a JSON-RPC body", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		body := make([]byte, 0, 4096)
		buf := make([]byte, 32<<10)
		limited := http.MaxBytesReader(w, r.Body, 4<<20)
		for {
			n, err := limited.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		resp := mcp.HandleBytes(g, body)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	})

	if token == "" {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			auth := r.Header.Get("Authorization")
			if !strings.EqualFold(auth, "Bearer "+token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func intParam(r *http.Request, name string, def int) int {
	if s := r.URL.Query().Get(name); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return def
}
