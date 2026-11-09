package export

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/neko233-com/graphify233/internal/model"
)

// ---------------------------------------------------------------- Cypher ----

// WriteCypher writes Neo4j-compatible CREATE/MERGE statements.
func WriteCypher(path string, g *model.Graph) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	fmt.Fprintf(w, "// graphify233 export: %d nodes, %d edges\n", len(g.Nodes), len(g.Edges))
	for _, n := range g.Nodes {
		fmt.Fprintf(w, "MERGE (n:%s {id: %s}) SET n.label = %s, n.file_type = %s, n.source_file = %s, n.source_location = %s, n.community = %d;\n",
			cypherLabel(n.FileType), cypherStr(n.ID), cypherStr(n.Label), cypherStr(n.FileType),
			cypherStr(n.SourceFile), cypherStr(n.SourceLocation), n.Community)
	}
	for _, e := range g.Edges {
		fmt.Fprintf(w, "MATCH (a {id: %s}), (b {id: %s}) MERGE (a)-[r:%s]->(b) SET r.confidence = %s, r.weight = %g, r.source_file = %s;\n",
			cypherStr(e.Source), cypherStr(e.Target), cypherLabel(e.Relation), cypherStr(e.Confidence), e.Weight, cypherStr(e.SourceFile))
	}
	return w.Flush()
}

func cypherLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "Unknown"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "N_" + out
	}
	return out
}

func cypherStr(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
}

// ---------------------------------------------------------------- GraphML ---

type graphmlDoc struct {
	XMLName xml.Name     `xml:"graphml"`
	Xmlns   string       `xml:"xmlns,attr"`
	Keys    []graphmlKey `xml:"key"`
	Graph   graphmlGraph `xml:"graph"`
}

type graphmlKey struct {
	ID      string `xml:"id,attr"`
	For     string `xml:"for,attr"`
	Name    string `xml:"attrName,attr"`
	Type    string `xml:"attr.type,attr"`
	Default string `xml:"default,omitempty"`
}

type graphmlGraph struct {
	ID          string        `xml:"id,attr"`
	EdgeDefault string        `xml:"edgedefault,attr"`
	Nodes       []graphmlNode `xml:"node"`
	Edges       []graphmlEdge `xml:"edge"`
}

type graphmlNode struct {
	ID   string        `xml:"id,attr"`
	Data []graphmlData `xml:"data"`
}

type graphmlEdge struct {
	Source string        `xml:"source,attr"`
	Target string        `xml:"target,attr"`
	Data   []graphmlData `xml:"data"`
}

type graphmlData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// WriteGraphML writes a GraphML document (yEd / Gephi compatible).
func WriteGraphML(path string, g *model.Graph, comms [][]int32, labels map[int]string) error {
	doc := graphmlDoc{
		Xmlns: "http://graphml.graphdrawing.org/xmlns",
		Keys: []graphmlKey{
			{ID: "label", For: "node", Name: "label", Type: "string"},
			{ID: "file_type", For: "node", Name: "file_type", Type: "string"},
			{ID: "source_file", For: "node", Name: "source_file", Type: "string"},
			{ID: "source_location", For: "node", Name: "source_location", Type: "string"},
			{ID: "community", For: "node", Name: "community", Type: "int"},
			{ID: "community_name", For: "node", Name: "community_name", Type: "string"},
			{ID: "degree", For: "node", Name: "degree", Type: "int"},
			{ID: "relation", For: "edge", Name: "relation", Type: "string"},
			{ID: "confidence", For: "edge", Name: "confidence", Type: "string"},
			{ID: "weight", For: "edge", Name: "weight", Type: "double"},
			{ID: "edge_source_file", For: "edge", Name: "source_file", Type: "string"},
		},
		Graph: graphmlGraph{ID: "G", EdgeDefault: "undirected"},
	}
	for i, n := range g.Nodes {
		commName := ""
		if n.Community >= 0 {
			commName = labels[n.Community]
		}
		doc.Graph.Nodes = append(doc.Graph.Nodes, graphmlNode{ID: n.ID, Data: []graphmlData{
			{Key: "label", Value: n.Label},
			{Key: "file_type", Value: n.FileType},
			{Key: "source_file", Value: n.SourceFile},
			{Key: "source_location", Value: n.SourceLocation},
			{Key: "community", Value: fmt.Sprint(n.Community)},
			{Key: "community_name", Value: commName},
			{Key: "degree", Value: fmt.Sprint(g.Degree(int32(i)))},
		}})
	}
	for _, e := range g.Edges {
		doc.Graph.Edges = append(doc.Graph.Edges, graphmlEdge{
			Source: e.Source, Target: e.Target,
			Data: []graphmlData{
				{Key: "relation", Value: e.Relation},
				{Key: "confidence", Value: e.Confidence},
				{Key: "weight", Value: fmt.Sprintf("%g", e.Weight)},
				{Key: "edge_source_file", Value: e.SourceFile},
			},
		})
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out := append([]byte(xml.Header), data...)
	return os.WriteFile(path, out, 0o644)
}

// ---------------------------------------------------------------- SVG -------

// WriteSVG writes a static SVG of the graph using the deterministic layout.
func WriteSVG(path string, g *model.Graph, comms [][]int32, labels map[int]string) error {
	pts := Layout(g, comms)
	const w, h = 1600.0, 1000.0
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g">`, w, h, w, h)
	b.WriteString(`<rect width="100%" height="100%" fill="#0e1116"/>`)
	b.WriteString(`<g stroke-linecap="round">`)
	for _, e := range g.Edges {
		s, t := g.Index(e.Source), g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		opacity := 0.30
		if e.Confidence == model.ConfInferred {
			opacity = 0.16
		} else if e.Confidence == model.ConfAmbiguous {
			opacity = 0.08
		}
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#8fa3bf" stroke-opacity="%.2f" stroke-width="1"/>`,
			pts[s].X, pts[s].Y, pts[t].X, pts[t].Y, opacity)
	}
	b.WriteString(`</g><g font-family="Segoe UI, Helvetica, Arial, sans-serif">`)
	maxDeg := 1
	for i := range g.Nodes {
		if d := g.Degree(int32(i)); d > maxDeg {
			maxDeg = d
		}
	}
	for i, n := range g.Nodes {
		deg := g.Degree(int32(i))
		r := 3.0 + 9.0*math.Sqrt(float64(deg)/float64(maxDeg))
		color := communityColor(n.Community)
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s"><title>%s (%s %s) — %d connections</title></circle>`,
			pts[i].X, pts[i].Y, r, color, xmlEscape(n.Label), xmlEscape(n.SourceFile), xmlEscape(n.SourceLocation), deg)
		if deg*100 >= maxDeg*12 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#d7e0ea" font-size="11">%s</text>`,
				pts[i].X+r+2, pts[i].Y+4, xmlEscape(n.Label))
		}
	}
	for ci, c := range comms {
		if len(c) == 0 {
			continue
		}
		name := labels[ci]
		if name == "" {
			name = fmt.Sprintf("Community %d", ci)
		}
		_ = name
	}
	b.WriteString(`</g></svg>`)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ---------------------------------------------------------------- Canvas ----

type canvasDoc struct {
	Nodes []canvasNode `json:"nodes"`
	Edges []canvasEdge `json:"edges"`
}

type canvasNode struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Color  string `json:"color,omitempty"`
}

type canvasEdge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	ToNode   string `json:"toNode"`
	Label    string `json:"label,omitempty"`
	Color    string `json:"color,omitempty"`
}

// WriteCanvas writes an Obsidian JSON Canvas file.
func WriteCanvas(path string, g *model.Graph, comms [][]int32, labels map[int]string) error {
	pts := Layout(g, comms)
	doc := canvasDoc{}
	for i, n := range g.Nodes {
		text := "**" + n.Label + "**"
		if n.SourceFile != "" {
			text += "\n`" + n.SourceFile + "`"
		}
		doc.Nodes = append(doc.Nodes, canvasNode{
			ID: n.ID, Type: "text", Text: text,
			X: int(math.Round(pts[i].X * 1.4)), Y: int(math.Round(pts[i].Y * 1.4)),
			Width: 220, Height: 70, Color: fmt.Sprint((n.Community % 6) + 1),
		})
	}
	for i, e := range g.Edges {
		doc.Edges = append(doc.Edges, canvasEdge{ID: fmt.Sprintf("e%d", i), FromNode: e.Source, ToNode: e.Target, Label: e.Relation})
	}
	data, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ---------------------------------------------------------------- Neo4j -----

// Neo4jConfig configures a push to a running Neo4j instance.
type Neo4jConfig struct {
	URL      string // e.g. http://localhost:7474
	Database string // default "neo4j"
	User     string
	Password string
	Batch    int // statements per transaction, default 200
}

type neo4jStatement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type neo4jRequest struct {
	Statements []neo4jStatement `json:"statements"`
}

// PushNeo4j loads the graph into Neo4j over the HTTP transaction API.
func PushNeo4j(g *model.Graph, cfg Neo4jConfig) (int, error) {
	if cfg.URL == "" {
		cfg.URL = "http://localhost:7474"
	}
	if cfg.Database == "" {
		cfg.Database = "neo4j"
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 200
	}
	endpoint := strings.TrimRight(cfg.URL, "/") + "/db/" + cfg.Database + "/tx/commit"
	client := &http.Client{Timeout: 30 * time.Second}
	sent := 0

	nodes := make([]neo4jStatement, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, neo4jStatement{
			Statement: "MERGE (n {id: $id}) SET n.label=$label, n.file_type=$ft, n.source_file=$sf, n.source_location=$sl, n.community=$c",
			Parameters: map[string]any{"id": n.ID, "label": n.Label, "ft": n.FileType,
				"sf": n.SourceFile, "sl": n.SourceLocation, "c": n.Community},
		})
	}
	edges := make([]neo4jStatement, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, neo4jStatement{
			Statement: "MATCH (a {id:$s}), (b {id:$t}) MERGE (a)-[r:RELATED {relation:$rel}]->(b) SET r.confidence=$conf, r.weight=$w, r.source_file=$sf",
			Parameters: map[string]any{"s": e.Source, "t": e.Target, "rel": e.Relation,
				"conf": e.Confidence, "w": e.Weight, "sf": e.SourceFile},
		})
	}
	all := append(nodes, edges...)
	for start := 0; start < len(all); start += cfg.Batch {
		end := start + cfg.Batch
		if end > len(all) {
			end = len(all)
		}
		body, err := json.Marshal(neo4jRequest{Statements: all[start:end]})
		if err != nil {
			return sent, err
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return sent, err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.User != "" {
			req.SetBasicAuth(cfg.User, cfg.Password)
		}
		resp, err := client.Do(req)
		if err != nil {
			return sent, err
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return sent, fmt.Errorf("neo4j returned HTTP %d", resp.StatusCode)
		}
		sent += end - start
	}
	return sent, nil
}

// CommunityStats is exposed for CLI summaries.
func CommunityStats(g *model.Graph) (int, []int) {
	counts := map[int]int{}
	for _, n := range g.Nodes {
		counts[n.Community]++
	}
	ids := make([]int, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return len(ids), ids
}
