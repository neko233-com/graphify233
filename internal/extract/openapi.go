package extract

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

var openAPIMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

type oaOperation struct {
	method string
	path   string
	line   int
}

type oaJSON struct {
	OpenAPI string `json:"openapi"`
	Swagger string `json:"swagger"`
	Info    struct {
		Title string `json:"title"`
	} `json:"info"`
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

// looksLikeOpenAPI sniffs a JSON or YAML document for an OpenAPI/Swagger
// marker. It is used by detection before a file is treated as code.
func looksLikeOpenAPI(data []byte) bool {
	head := data
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	lower := strings.ToLower(string(head))
	if strings.Contains(lower, "\"openapi\"") || strings.Contains(lower, "\"swagger\"") {
		return true
	}
	for _, line := range splitLines(string(head)) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "openapi:") || strings.HasPrefix(t, "swagger:") {
			return true
		}
	}
	return false
}

// extractOpenAPI turns an OpenAPI/Swagger description into an API node and one
// node per path+method operation. JSON and the common YAML layout are both
// supported without external dependencies.
func extractOpenAPI(path, stem string) model.Extraction {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Extraction{}
	}
	b := NewBuilder(path, stem, model.FileTypeCode)
	if !looksLikeOpenAPI(data) {
		return b.Result()
	}
	title := ""
	var ops []oaOperation
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		var doc oaJSON
		if err := json.Unmarshal(data, &doc); err == nil && (doc.OpenAPI != "" || doc.Swagger != "") {
			title = doc.Info.Title
			paths := make([]string, 0, len(doc.Paths))
			for p := range doc.Paths {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				methods := make([]string, 0, len(doc.Paths[p]))
				for m := range doc.Paths[p] {
					if openAPIMethods[m] {
						methods = append(methods, m)
					}
				}
				sort.Strings(methods)
				for _, m := range methods {
					ops = append(ops, oaOperation{method: m, path: p, line: 1})
				}
			}
		}
	} else {
		title, ops = scanOpenAPIYAML(string(data))
	}
	if title == "" && len(ops) == 0 {
		return b.Result()
	}
	if title == "" {
		title = stem
	}
	apiID := MakeID(stem, "api")
	label := strings.TrimSpace(title)
	if !strings.HasSuffix(strings.ToLower(label), "api") {
		label += " API"
	}
	b.AddNode(apiID, label, 1)
	b.Edge(b.FileID, apiID, "contains", 1)
	for _, op := range ops {
		id := MakeID(stem, op.method, op.path)
		b.AddNode(id, strings.ToUpper(op.method)+" "+op.path, op.line)
		b.Edge(b.FileID, id, "contains", op.line)
	}
	return b.Result()
}

func scanOpenAPIYAML(text string) (string, []oaOperation) {
	title := ""
	var ops []oaOperation
	inPaths := false
	path := ""
	for i, raw := range splitLines(text) {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "openapi:") || strings.HasPrefix(t, "swagger:") {
			continue
		}
		if indent == 0 {
			inPaths = strings.HasPrefix(t, "paths:")
			if inPaths {
				path = ""
			}
			continue
		}
		if !inPaths {
			if strings.HasPrefix(t, "title:") && indent == 2 {
				title = strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "title:")), `"'`)
			}
			continue
		}
		if indent == 2 && strings.HasPrefix(t, "/") {
			path = strings.TrimSuffix(t, ":")
			continue
		}
		if indent >= 4 && path != "" && strings.HasSuffix(t, ":") {
			method := strings.ToLower(strings.TrimSuffix(t, ":"))
			if openAPIMethods[method] {
				ops = append(ops, oaOperation{method: method, path: path, line: i + 1})
			}
		}
	}
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].path != ops[j].path {
			return ops[i].path < ops[j].path
		}
		return ops[i].method < ops[j].method
	})
	return title, ops
}
