// Package enrich adds deterministic cross-modal edges that the per-file
// extractors cannot see on their own. It is used by --mode deep: documentation
// and papers mention code symbols by name, and those mentions become INFERRED
// edges. No LLM, no network, fully reproducible.
package enrich

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/pdf"
)

// Doc is a documentation or paper file that can mention code symbols.
type Doc struct {
	Path string // filesystem path
	Rel  string // slash-separated path stored on nodes
	Kind model.FileType
}

// Limits keeps the enrichment bounded on large corpora.
type Limits struct {
	PerDoc   int // mentions added per document, default 200
	Total    int // mentions added overall, default 20000
	MaxBytes int64
}

func (l Limits) withDefaults() Limits {
	if l.PerDoc <= 0 {
		l.PerDoc = 200
	}
	if l.Total <= 0 {
		l.Total = 20000
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = 8 << 20
	}
	return l
}

// docsIndex locates the graph node that represents a documentation file. Node
// source paths can be absolute or relative, so the lookup tries the exact
// slash-separated path, then a suffix match, then a unique base name.
type docsIndex struct {
	exact map[string]int32
	base  map[string]int32
	dup   map[string]bool
}

func newDocsIndex(n int) *docsIndex {
	return &docsIndex{
		exact: make(map[string]int32, n),
		base:  make(map[string]int32, n),
		dup:   make(map[string]bool, 4),
	}
}

func (d *docsIndex) add(sourceFile string, idx int32) {
	slash := strings.ToLower(filepath.ToSlash(sourceFile))
	if _, ok := d.exact[slash]; !ok {
		d.exact[slash] = idx
	}
	b := strings.ToLower(baseName(sourceFile))
	if _, ok := d.base[b]; ok {
		d.dup[b] = true
		return
	}
	d.base[b] = idx
}

func (d *docsIndex) find(rel string) (int32, bool) {
	slash := strings.ToLower(filepath.ToSlash(rel))
	if i, ok := d.exact[slash]; ok {
		return i, true
	}
	if strings.HasPrefix(slash, "../") {
		slash = strings.TrimPrefix(slash, "../")
	}
	b := strings.ToLower(baseName(slash))
	if i, ok := d.base[b]; ok && !d.dup[b] {
		return i, true
	}
	suffix := "/" + slash
	for p, i := range d.exact {
		if strings.HasSuffix(p, suffix) {
			return i, true
		}
	}
	return -1, false
}

// Mentions adds "mentions" edges from documentation file nodes to uniquely named
// code symbols found in their text. It returns the number of edges added.
func Mentions(g *model.Graph, docs []Doc, lim Limits) int {
	lim = lim.withDefaults()
	if len(g.Nodes) == 0 || len(docs) == 0 {
		return 0
	}
	// Unique code symbols by normalised label, and file node lookup by source.
	byLabel := make(map[string]int32, len(g.Nodes)/2)
	ambiguous := make(map[string]bool, 8)
	fileNode := newDocsIndex(len(docs))
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.SourceFile != "" && strings.EqualFold(n.Label, baseName(n.SourceFile)) {
			fileNode.add(n.SourceFile, int32(i))
		}
		if n.FileType != model.FileTypeCode || n.SourceFile == "" || model.IsFileExtLabel(n.Label) {
			continue
		}
		key := Normalize(n.Label)
		if len(key) < 3 {
			continue
		}
		if _, exists := byLabel[key]; exists {
			ambiguous[key] = true
			continue
		}
		byLabel[key] = int32(i)
	}
	for key := range ambiguous {
		delete(byLabel, key)
	}
	if len(byLabel) == 0 {
		return 0
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].Rel < docs[j].Rel })
	added := 0
	for _, d := range docs {
		if added >= lim.Total {
			break
		}
		src, ok := fileNode.find(d.Rel)
		if !ok {
			continue
		}
		text, err := read(d, lim.MaxBytes)
		if err != nil || text == "" {
			continue
		}
		perDoc := 0
		seen := make(map[int32]bool, 8)
		for lineNo, line := range splitLines(text) {
			for _, tok := range identifiers(line) {
				key := strings.ToLower(tok)
				idx, ok := byLabel[key]
				if !ok || seen[idx] {
					continue
				}
				target := g.Nodes[idx]
				if target.ID == g.Nodes[src].ID || g.HasEdge(g.Nodes[src].ID, target.ID) {
					continue
				}
				seen[idx] = true
				if g.AddEdge(model.Edge{
					Source:          g.Nodes[src].ID,
					Target:          target.ID,
					Relation:        "mentions",
					Confidence:      model.ConfInferred,
					SourceFile:      d.Rel,
					SourceLocation:  fmt.Sprintf("L%d", lineNo+1),
					Weight:          0.6,
					ConfidenceScore: 0.6,
				}) {
					added++
					perDoc++
					if perDoc >= lim.PerDoc || added >= lim.Total {
						break
					}
				}
			}
			if perDoc >= lim.PerDoc || added >= lim.Total {
				break
			}
		}
	}
	return added
}

// Normalize turns a code label ("name()", ".method()", "Type") into the
// lowercase identifier used for matching.
func Normalize(label string) string {
	label = strings.TrimSuffix(label, "()")
	label = strings.TrimPrefix(label, ".")
	return strings.ToLower(label)
}

func read(d Doc, max int64) (string, error) {
	if d.Kind == model.FileTypePaper {
		return pdf.ExtractText(d.Path)
	}
	fi, err := os.Stat(d.Path)
	if err != nil {
		return "", err
	}
	if fi.Size() > max {
		return "", fmt.Errorf("document too large")
	}
	b, err := os.ReadFile(d.Path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// identifiers yields ASCII identifier tokens of length >= 3. Code symbols in
// the graph are ASCII, so limiting the scan keeps it fast.
func identifiers(line string) []string {
	var out []string
	for i := 0; i < len(line); {
		if !isIdentStart(line[i]) {
			i++
			continue
		}
		start := i
		for i < len(line) && isIdentPart(line[i]) {
			i++
		}
		if i-start >= 3 {
			out = append(out, line[start:i])
		}
	}
	return out
}
