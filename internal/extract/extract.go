// Package extract turns source files into graph nodes and edges.
//
// Every extractor is deterministic: the same bytes always produce the same
// nodes, edges and source locations. Edges found by parsing carry EXTRACTED;
// call-graph edges are INFERRED because they rely on name resolution.
package extract

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// Version is bumped whenever extraction output changes, invalidating caches.
const Version = 3

// MakeID builds a stable, URL/file friendly node id from name parts.
func MakeID(parts ...string) string {
	var joined strings.Builder
	for _, p := range parts {
		p = strings.Trim(p, "_.")
		if p == "" {
			continue
		}
		if joined.Len() > 0 {
			joined.WriteByte('_')
		}
		joined.WriteString(p)
	}
	src := joined.String()
	var out strings.Builder
	out.Grow(len(src))
	prevSep := false
	for _, r := range src {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep {
			out.WriteByte('_')
			prevSep = true
		}
	}
	id := strings.Trim(out.String(), "_")
	if id == "" {
		return "unnamed"
	}
	return strings.ToLower(id)
}

// Builder accumulates nodes and edges for a single file.
type Builder struct {
	Path     string
	Stem     string
	FileType string
	FileID   string

	nodes    []model.Node
	edges    []model.Edge
	seen     map[string]struct{}
	labels   map[string]string
	callSeen map[string]struct{}
}

// NewBuilder creates a builder and seeds the file-level node.
func NewBuilder(path, stem, fileType string) *Builder {
	b := &Builder{
		Path:     path,
		Stem:     stem,
		FileType: fileType,
		FileID:   MakeID(stem),
		seen:     make(map[string]struct{}, 32),
		labels:   make(map[string]string, 32),
		callSeen: make(map[string]struct{}, 32),
	}
	b.AddNode(b.FileID, filepath.Base(path), 1)
	return b
}

// AddNode records a node once per id.
func (b *Builder) AddNode(id, label string, line int) {
	if id == "" {
		return
	}
	if _, ok := b.seen[id]; ok {
		return
	}
	b.seen[id] = struct{}{}
	b.labels[normaliseLabel(label)] = id
	b.nodes = append(b.nodes, model.Node{
		ID:             id,
		Label:          label,
		FileType:       b.FileType,
		SourceFile:     b.Path,
		SourceLocation: fmt.Sprintf("L%d", line),
		Community:      -1,
	})
}

// AddStub records an external/unresolved entity so an edge survives.
func (b *Builder) AddStub(id, label string) {
	if _, ok := b.seen[id]; ok {
		return
	}
	b.seen[id] = struct{}{}
	b.labels[normaliseLabel(label)] = id
	b.nodes = append(b.nodes, model.Node{
		ID:        id,
		Label:     label,
		FileType:  b.FileType,
		Community: -1,
	})
}

func normaliseLabel(label string) string {
	return strings.ToLower(strings.TrimLeft(strings.Trim(label, "()"), "."))
}

// Edge records an EXTRACTED relationship.
func (b *Builder) Edge(src, tgt, relation string, line int) {
	b.EdgeConf(src, tgt, relation, line, model.ConfExtracted, 1.0)
}

// EdgeConf records a relationship with an explicit confidence.
func (b *Builder) EdgeConf(src, tgt, relation string, line int, confidence string, weight float64) {
	if confidence == "" {
		confidence = model.ConfExtracted
	}
	if weight == 0 {
		weight = 1
	}
	b.edges = append(b.edges, model.Edge{
		Source:          src,
		Target:          tgt,
		Relation:        relation,
		Confidence:      confidence,
		SourceFile:      b.Path,
		SourceLocation:  fmt.Sprintf("L%d", line),
		Weight:          weight,
		ConfidenceScore: model.DefaultConfidenceScore(confidence),
	})
}

// Call records a deduplicated INFERRED call edge.
func (b *Builder) Call(caller, callee, calleeName string, line int) {
	if callee == "" || callee == caller {
		return
	}
	key := caller + "\x00" + callee
	if _, ok := b.callSeen[key]; ok {
		return
	}
	b.callSeen[key] = struct{}{}
	b.EdgeConf(caller, callee, "calls", line, model.ConfInferred, 0.8)
}

// Lookup resolves a label (normalised) to a node id.
func (b *Builder) Lookup(label string) string {
	return b.labels[strings.ToLower(label)]
}

// Result filters dangling edges and returns the extraction payload.
func (b *Builder) Result() model.Extraction {
	clean := b.edges[:0]
	for _, e := range b.edges {
		_, srcOK := b.seen[e.Source]
		_, tgtOK := b.seen[e.Target]
		if !srcOK {
			continue
		}
		if !tgtOK && e.Relation != "imports" && e.Relation != "imports_from" {
			continue
		}
		clean = append(clean, e)
	}
	return model.Extraction{Nodes: b.nodes, Edges: clean}
}

// File dispatches extraction for a single file. path is the display path
// (forward slashes, relative to the invocation root).
func File(path string, kind model.FileType) model.Extraction {
	ext := strings.ToLower(filepath.Ext(path))
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	switch ext {
	case ".py":
		return extractPython(path, stem)
	case ".go":
		return extractGo(path, stem)
	case ".js", ".jsx", ".ts", ".tsx":
		return extractJS(path, stem, ext)
	case ".rs":
		return extractRust(path, stem)
	case ".java":
		return extractJava(path, stem)
	case ".c", ".h":
		return extractC(path, stem, ext)
	case ".cc", ".cpp", ".cxx", ".hpp":
		return extractCPP(path, stem)
	case ".rb":
		return extractRuby(path, stem)
	case ".cs":
		return extractCSharp(path, stem)
	case ".kt", ".kts":
		return extractKotlin(path, stem)
	case ".scala":
		return extractScala(path, stem)
	case ".php":
		return extractPHP(path, stem)
	case ".md", ".txt", ".rst":
		return extractDoc(path, stem, kind)
	case ".pdf":
		return extractPaper(path, stem)
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return extractImage(path, stem)
	}
	b := NewBuilder(path, stem, model.FileTypeCode)
	return b.Result()
}

// tokenizer helpers ---------------------------------------------------------

func isPunct(t lex.Token, s string) bool { return t.Kind == lex.Punct && t.Text == s }
func isIdent(t lex.Token, s string) bool { return t.Kind == lex.Ident && t.Text == s }

// matchBlock finds the index of the closing token for the opening token at i.
func matchBlock(toks []lex.Token, i int) int {
	open := toks[i].Text
	close := ""
	switch open {
	case "{":
		close = "}"
	case "(":
		close = ")"
	case "[":
		close = "]"
	default:
		return -1
	}
	depth := 0
	for j := i; j < len(toks); j++ {
		if toks[j].Kind != lex.Punct {
			continue
		}
		if toks[j].Text == open {
			depth++
		} else if toks[j].Text == close {
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// findBody finds the opening brace of a declaration body starting at i.
// It stops at a statement terminator to avoid walking into the next decl.
func findBody(toks []lex.Token, i int) int {
	depth := 0
	for j := i; j < len(toks); j++ {
		if toks[j].Kind != lex.Punct {
			continue
		}
		switch toks[j].Text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case "{":
			if depth == 0 {
				return j
			}
		case ";":
			if depth == 0 {
				return -1
			}
		case "}":
			if depth == 0 {
				return -1
			}
		}
	}
	return -1
}

// FuncSpan is a function body span used for call attribution.
type FuncSpan struct {
	ID    string
	Start int // index of '{'
	End   int // index of matching '}'
}

// ScanCalls attributes call expressions inside each span. Calls that appear
// inside nested braces of a nested function literal are skipped via skipFunc.
func ScanCalls(toks []lex.Token, spans []FuncSpan, b *Builder, skip func(i int) int) {
	for _, sp := range spans {
		for i := sp.Start + 1; i < sp.End; i++ {
			if skip != nil {
				if jump := skip(i); jump > i && jump < sp.End {
					i = jump
					continue
				}
			}
			t := toks[i]
			if t.Kind != lex.Ident {
				continue
			}
			if i+1 >= sp.End || !isPunct(toks[i+1], "(") {
				continue
			}
			// Calls reached through a selector/namespace are always real:
			// `Graph::new()` and `this.get()` must survive keyword filtering.
			selector := i > 0 && toks[i-1].Kind == lex.Punct &&
				(toks[i-1].Text == "." || toks[i-1].Text == "?." ||
					toks[i-1].Text == "::" || toks[i-1].Text == "->")
			if !selector {
				// Avoid keywords that look like calls.
				if callKeywords[t.Text] {
					continue
				}
				// `new Foo()` constructs a value; it is not a call to Foo.
				if i > 0 && toks[i-1].Kind == lex.Ident {
					switch toks[i-1].Text {
					case "new", "function", "func", "fn", "def", "class":
						continue
					}
				}
			}
			name := t.Text
			if i+3 < sp.End && (isPunct(toks[i+1], ".") || isPunct(toks[i+1], "?.")) && toks[i+2].Kind == lex.Ident && isPunct(toks[i+3], "(") {
				name = toks[i+2].Text
			}
			if id := b.Lookup(name); id != "" {
				b.Call(sp.ID, id, name, int(t.Line))
			}
		}
	}
}

var callKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "func": true, "function": true, "def": true, "fn": true,
	"new": true, "delete": true, "typeof": true, "sizeof": true, "super": true,
	"this": true, "self": true, "match": true, "when": true, "until": true,
	"foreach": true, "using": true, "lock": true, "fixed": true, "sizeof_": true,
}
