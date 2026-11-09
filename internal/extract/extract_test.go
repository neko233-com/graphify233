package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

type goldenNode struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	FileType       string `json:"file_type"`
	SourceLocation string `json:"source_location"`
}

type goldenEdge struct {
	Source         string  `json:"source"`
	Target         string  `json:"target"`
	Relation       string  `json:"relation"`
	Confidence     string  `json:"confidence"`
	SourceLocation string  `json:"source_location"`
	Weight         float64 `json:"weight"`
}

type goldenFile struct {
	Nodes []goldenNode `json:"nodes"`
	Edges []goldenEdge `json:"edges"`
}

func loadGolden(t *testing.T, name string) goldenFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

// TestGoldenParity compares our deterministic extraction against the output of
// the Python reference (generated once by tools/gen_goldens.py and committed).
func TestGoldenParity(t *testing.T) {
	cases := []struct{ file, golden string }{
		{"sample.py", "expected_py.json"},
		{"sample_calls.py", "expected_calls.json"},
		{"sample.go", "expected_go.json"},
		{"sample.ts", "expected_ts.json"},
		{"sample.rs", "expected_rs.json"},
		{"sample.java", "expected_java.json"},
		{"sample.c", "expected_c.json"},
		{"sample.cpp", "expected_cpp.json"},
		{"sample.cs", "expected_cs.json"},
		{"sample.kt", "expected_kt.json"},
		{"sample.scala", "expected_scala.json"},
		{"sample.php", "expected_php.json"},
		{"sample.rb", "expected_rb.json"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got := File(filepath.Join("testdata", tc.file), model.FileTypeCode)
			want := loadGolden(t, tc.golden)
			compareNodes(t, got.Nodes, want.Nodes)
			compareEdges(t, got.Edges, want.Edges)
		})
	}
}

func compareNodes(t *testing.T, got []model.Node, want []goldenNode) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("node count = %d, want %d", len(got), len(want))
	}
	for i := 0; i < len(got) && i < len(want); i++ {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Label != w.Label || g.FileType != w.FileType || g.SourceLocation != w.SourceLocation {
			t.Errorf("node[%d] = {%s %q %s %s}, want {%s %q %s %s}",
				i, g.ID, g.Label, g.FileType, g.SourceLocation,
				w.ID, w.Label, w.FileType, w.SourceLocation)
		}
	}
}

func compareEdges(t *testing.T, got []model.Edge, want []goldenEdge) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("edge count = %d, want %d", len(got), len(want))
	}
	for i := 0; i < len(got) && i < len(want); i++ {
		g, w := got[i], want[i]
		if g.Source != w.Source || g.Target != w.Target || g.Relation != w.Relation ||
			g.Confidence != w.Confidence || g.SourceLocation != w.SourceLocation || g.Weight != w.Weight {
			t.Errorf("edge[%d] = {%s -> %s %s %s %s %v}, want {%s -> %s %s %s %s %v}",
				i, g.Source, g.Target, g.Relation, g.Confidence, g.SourceLocation, g.Weight,
				w.Source, w.Target, w.Relation, w.Confidence, w.SourceLocation, w.Weight)
		}
	}
}
