package extract

import (
	"os"
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractPHP scans `use` imports (last namespace segment), classes and their
// methods, and top-level functions. Method bodies are scanned for calls.
func extractPHP(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.CLike)
	var spans []FuncSpan
	phpDecls(b, src, toks, 0, len(toks), "", &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func phpDecls(b *Builder, src string, toks []lex.Token, start, end int, cid string, spans *[]FuncSpan) {
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
		case "use":
			line := int(t.Line)
			stop := stmtEnd(toks, i+1, end)
			if stop > i+1 {
				raw := strings.TrimSpace(src[toks[i+1].Start:toks[stop-1].End])
				parts := strings.Split(raw, "\\")
				target := strings.TrimSpace(parts[len(parts)-1])
				target = strings.TrimSuffix(target, "}");
				if target != "" {
					b.Edge(b.FileID, MakeID(target), "imports", line)
				}
			}
			i = stop
		case "namespace":
			i = lineEnd(toks, i, end)
		case "class", "interface", "trait", "enum":
			if i+1 >= end || toks[i+1].Kind != lex.Ident {
				continue
			}
			name := toks[i+1].Text
			line := int(t.Line)
			nid := MakeID(b.Stem, name)
			b.AddNode(nid, name, line)
			b.Edge(b.FileID, nid, "contains", line)
			open := bodyFor(toks, i+2, end, line)
			if open < 0 {
				i++
				continue
			}
			close := matchBlock(toks, open)
			if close > open {
				phpMembers(b, toks, open, close, nid, spans)
				i = close
			}
		case "function":
			i = phpFunc(b, toks, i, end, cid, spans)
		}
	}
}

func phpMembers(b *Builder, toks []lex.Token, open, close int, cid string, spans *[]FuncSpan) {
	d := 0
	for k := open + 1; k < close; k++ {
		t := toks[k]
		if t.Kind == lex.Punct {
			switch t.Text {
			case "{", "(", "[":
				d++
			case "}", ")", "]":
				if d > 0 {
					d--
				}
			}
			continue
		}
		if d != 0 || t.Kind != lex.Ident || t.Text != "function" {
			continue
		}
		k = phpFunc(b, toks, k, close, cid, spans)
	}
}

// phpFunc records a function or method declaration starting at the `function`
// keyword and returns the index of its body's closing brace.
func phpFunc(b *Builder, toks []lex.Token, i, end int, cid string, spans *[]FuncSpan) int {
	j := i + 1
	for j < end && isPunct(toks[j], "&") {
		j++
	}
	if j >= end || toks[j].Kind != lex.Ident {
		return i
	}
	name := toks[j].Text
	line := int(toks[i].Line)
	var fid string
	if cid != "" {
		fid = MakeID(cid, name)
		b.AddNode(fid, "."+name+"()", line)
		b.Edge(cid, fid, "method", line)
	} else {
		fid = MakeID(b.Stem, name)
		b.AddNode(fid, name+"()", line)
		b.Edge(b.FileID, fid, "contains", line)
	}
	params := j + 1
	if params >= end || !isPunct(toks[params], "(") {
		return j
	}
	paramsClose := matchBlock(toks, params)
	if paramsClose < 0 {
		return j
	}
	bodyOpen := bodyFor(toks, paramsClose+1, end, line)
	if bodyOpen < 0 {
		return paramsClose
	}
	bodyEnd := matchBlock(toks, bodyOpen)
	if bodyEnd <= bodyOpen {
		return bodyOpen
	}
	*spans = append(*spans, FuncSpan{ID: fid, Start: bodyOpen, End: bodyEnd})
	return bodyEnd
}
