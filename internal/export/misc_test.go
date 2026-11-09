package export

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func sampleGraph() *model.Graph {
	g := model.New()
	for _, n := range []model.Node{
		{ID: "a_py", Label: "a.py", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L1", Community: 0},
		{ID: "run", Label: "run()", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L3", Community: 0},
		{ID: "graph", Label: "Graph", FileType: model.FileTypeCode, SourceFile: "a.py", SourceLocation: "L7", Community: 0},
	} {
		g.AddNode(n)
	}
	g.AddEdge(model.Edge{Source: "a_py", Target: "run", Relation: "contains", Confidence: model.ConfExtracted, SourceFile: "a.py"})
	g.AddEdge(model.Edge{Source: "run", Target: "graph", Relation: "uses", Confidence: model.ConfInferred, SourceFile: "a.py"})
	return g
}

func TestWriteCypher(t *testing.T) {
	g := sampleGraph()
	path := filepath.Join(t.TempDir(), "graph.cypher")
	if err := WriteCypher(path, g); err != nil {
		t.Fatal(err)
	}
	text := readOut(t, path)
	for _, want := range []string{"MERGE", "a_py", "run", "contains"} {
		if !strings.Contains(text, want) {
			t.Fatalf("cypher missing %q:\n%s", want, text)
		}
	}
}

func TestWriteGraphMLHasUniqueKeys(t *testing.T) {
	g := sampleGraph()
	path := filepath.Join(t.TempDir(), "graph.graphml")
	if err := WriteGraphML(path, g, [][]int32{{0, 1, 2}}, map[int]string{0: "core"}); err != nil {
		t.Fatal(err)
	}
	raw := readOut(t, path)
	var doc struct {
		XMLName xml.Name
		Keys    []struct {
			ID   string `xml:"id,attr"`
			For  string `xml:"for,attr"`
			Name string `xml:"name,attr"`
		} `xml:"key"`
		Nodes []struct {
			ID string `xml:"id,attr"`
		} `xml:"graph>node"`
		Edges []struct {
			Source string `xml:"source,attr"`
			Target string `xml:"target,attr"`
		} `xml:"graph>edge"`
	}
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("graphml is not valid XML: %v\n%s", err, raw)
	}
	if doc.XMLName.Local != "graphml" {
		t.Fatalf("root element = %q", doc.XMLName.Local)
	}
	ids := map[string]string{}
	for _, k := range doc.Keys {
		if prev, dup := ids[k.ID]; dup {
			t.Fatalf("duplicate key id %q (for=%s and %s)", k.ID, prev, k.For)
		}
		ids[k.ID] = k.For
	}
	if len(doc.Nodes) != 3 || len(doc.Edges) != 2 {
		t.Fatalf("nodes=%d edges=%d, want 3/2", len(doc.Nodes), len(doc.Edges))
	}
}

func TestWriteSVG(t *testing.T) {
	g := sampleGraph()
	path := filepath.Join(t.TempDir(), "graph.svg")
	if err := WriteSVG(path, g, [][]int32{{0, 1, 2}}, map[int]string{0: "core"}); err != nil {
		t.Fatal(err)
	}
	text := readOut(t, path)
	if !strings.Contains(text, "<svg") || !strings.Contains(text, "</svg>") {
		t.Fatalf("not an svg document:\n%.200s", text)
	}
	if !strings.Contains(text, "run()") {
		t.Fatal("node label missing from svg")
	}
}

func TestWriteCanvas(t *testing.T) {
	g := sampleGraph()
	path := filepath.Join(t.TempDir(), "graph.canvas")
	if err := WriteCanvas(path, g, [][]int32{{0, 1, 2}}, map[int]string{0: "core"}); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Label string `json:"text"`
		} `json:"nodes"`
		Edges []struct {
			From string `json:"fromNode"`
			To   string `json:"toNode"`
		} `json:"edges"`
	}
	raw := readOut(t, path)
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("canvas is not valid JSON: %v", err)
	}
	if len(doc.Nodes) != 3 || len(doc.Edges) != 2 {
		t.Fatalf("nodes=%d edges=%d, want 3/2", len(doc.Nodes), len(doc.Edges))
	}
	for _, n := range doc.Nodes {
		if n.ID == "" {
			t.Fatal("canvas node without id")
		}
	}
}

func readOut(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
