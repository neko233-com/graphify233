package extract

import (
	"os"
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractRust walks top-level items: functions, structs/enums/traits and impl
// blocks whose functions become methods of the implemented type. `use`
// declarations become imports_from edges to the last path segment.
func extractRust(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.CLike)
	var spans []FuncSpan
	rustScope(b, src, toks, 0, len(toks), "", &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

// rustScope scans declaration-level tokens in [start,end) at brace depth 0.
func rustScope(b *Builder, src string, toks []lex.Token, start, end int, parentID string, spans *[]FuncSpan) {
	depth := 0
	for i := start; i < end; i++ {
		t := toks[i]
		if t.Kind == lex.Punct {
			switch t.Text {
			case "{", "(", "[":
				depth++
			case "}", ")", "]":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if depth != 0 || t.Kind != lex.Ident {
			continue
		}
		switch t.Text {
		case "fn":
			i = rustFunc(b, toks, i, parentID, spans)
		case "struct", "enum", "trait":
			if i+1 < end && toks[i+1].Kind == lex.Ident {
				name := toks[i+1].Text
				line := int(t.Line)
				nid := MakeID(b.Stem, name)
				b.AddNode(nid, name, line)
				b.Edge(b.FileID, nid, "contains", line)
				i++
			}
		case "impl":
			i = rustImpl(b, src, toks, i, end, spans)
		case "use":
			i = rustUse(b, src, toks, i, end)
		case "mod":
			// Inline modules still contribute their items at file scope.
			if open := findBody(toks, i); open >= 0 {
				if close := matchBlock(toks, open); close > open {
					rustScope(b, src, toks, open+1, close, "", spans)
					i = close
				}
			}
		}
	}
}

func rustFunc(b *Builder, toks []lex.Token, i int, parentID string, spans *[]FuncSpan) int {
	j := i + 1
	if j >= len(toks) || toks[j].Kind != lex.Ident {
		return i
	}
	name := toks[j].Text
	line := int(toks[i].Line)
	var fid string
	if parentID != "" {
		fid = MakeID(parentID, name)
		b.AddNode(fid, "."+name+"()", line)
		b.Edge(parentID, fid, "method", line)
	} else {
		fid = MakeID(b.Stem, name)
		b.AddNode(fid, name+"()", line)
		b.Edge(b.FileID, fid, "contains", line)
	}
	if body := findBody(toks, j+1); body >= 0 {
		if end := matchBlock(toks, body); end > body {
			*spans = append(*spans, FuncSpan{ID: fid, Start: body, End: end})
			return end
		}
	}
	return j
}

func rustImpl(b *Builder, src string, toks []lex.Token, i, end int, spans *[]FuncSpan) int {
	open := findBody(toks, i+1)
	if open < 0 || open >= end {
		return i
	}
	// The implemented type: after `for` when present, otherwise the first
	// identifier outside the leading generic parameter list.
	typeStart := -1
	ang := 0
	for k := i + 1; k < open; k++ {
		if toks[k].Kind == lex.Punct {
			switch toks[k].Text {
			case "<":
				ang++
			case ">":
				if ang > 0 {
					ang--
				}
			}
			continue
		}
		if toks[k].Kind != lex.Ident {
			continue
		}
		if toks[k].Text == "for" && ang == 0 {
			typeStart = k + 1
			break
		}
		if typeStart < 0 && ang == 0 && toks[k].Text != "impl" {
			typeStart = k
		}
	}
	if typeStart < 0 || typeStart >= open {
		return i
	}
	// Stop at a where-clause so only the type text is captured.
	typeStop := open
	ang = 0
	for k := typeStart; k < open; k++ {
		if toks[k].Kind == lex.Punct {
			switch toks[k].Text {
			case "<":
				ang++
			case ">":
				if ang > 0 {
					ang--
				}
			}
			continue
		}
		if ang == 0 && toks[k].Kind == lex.Ident && toks[k].Text == "where" {
			typeStop = k
			break
		}
	}
	raw := strings.TrimSpace(src[toks[typeStart].Start:toks[typeStop-1].End])
	if raw == "" {
		return i
	}
	implID := MakeID(b.Stem, raw)
	b.AddNode(implID, raw, int(toks[i].Line))
	close := matchBlock(toks, open)
	if close < 0 {
		return i
	}
	rustScope(b, src, toks, open+1, close, implID, spans)
	return close
}

func rustUse(b *Builder, src string, toks []lex.Token, i, end int) int {
	line := int(toks[i].Line)
	j := i + 1
	stop := j
	for ; stop < end; stop++ {
		if isPunct(toks[stop], ";") {
			break
		}
	}
	if stop >= end || stop == j {
		return i
	}
	raw := src[toks[j].Start:toks[stop-1].End]
	if k := strings.IndexByte(raw, '{'); k >= 0 {
		raw = raw[:k]
	}
	raw = strings.TrimRight(raw, ":* \n\t")
	parts := strings.Split(raw, "::")
	module := strings.TrimSpace(parts[len(parts)-1])
	if module == "" {
		return stop
	}
	b.Edge(b.FileID, MakeID(module), "imports_from", line)
	return stop
}
