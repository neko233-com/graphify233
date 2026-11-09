package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractCPP scans C++ source. Classes and structs become nodes; only
// constructors and destructors declared inside them become methods — inline
// member functions with a return type parse as field declarations in the
// reference tree-sitter grammar and are not extracted.
func extractCPP(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	includeHeader(b, src)
	toks := lex.Tokenize(src, lex.CLike)
	var spans []FuncSpan
	cppScope(b, toks, 0, len(toks), &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func cppScope(b *Builder, toks []lex.Token, start, end int, spans *[]FuncSpan) {
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
		if t.Text == "class" || t.Text == "struct" {
			if i+1 >= end || toks[i+1].Kind != lex.Ident {
				continue
			}
			name := toks[i+1].Text
			line := int(t.Line)
			cid := MakeID(b.Stem, name)
			b.AddNode(cid, name, line)
			b.Edge(b.FileID, cid, "contains", line)
			open := bodyFor(toks, i+2, end, line)
			if open < 0 {
				i++
				continue
			}
			close := matchBlock(toks, open)
			if close <= open {
				continue
			}
			cppClassBody(b, toks, open, close, cid, name, spans)
			i = close
			continue
		}
		if cReserved[t.Text] || i+1 >= end || !isPunct(toks[i+1], "(") {
			continue
		}
		paramsClose := matchBlock(toks, i+1)
		if paramsClose < 0 || paramsClose+1 >= end || !isPunct(toks[paramsClose+1], "{") {
			continue
		}
		line := int(t.Line)
		name := t.Text
		fid := MakeID(b.Stem, name)
		b.AddNode(fid, name+"()", line)
		b.Edge(b.FileID, fid, "contains", line)
		if bodyEnd := matchBlock(toks, paramsClose+1); bodyEnd > paramsClose {
			*spans = append(*spans, FuncSpan{ID: fid, Start: paramsClose + 1, End: bodyEnd})
			i = bodyEnd
		}
	}
}

// cppClassBody extracts constructors and destructors declared in a class body.
func cppClassBody(b *Builder, toks []lex.Token, open, close int, cid, cname string, spans *[]FuncSpan) {
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
		if d != 0 {
			continue
		}
		name := ""
		params := -1
		if t.Kind == lex.Ident && t.Text == cname && k+1 < close && isPunct(toks[k+1], "(") {
			name = cname
			params = k + 1
		}
		if isPunct(t, "~") && k+2 < close && toks[k+1].Kind == lex.Ident && toks[k+1].Text == cname && isPunct(toks[k+2], "(") {
			name = "~" + cname
			params = k + 2
		}
		if name == "" || params < 0 {
			continue
		}
		paramsClose := matchBlock(toks, params)
		if paramsClose < 0 {
			continue
		}
		bodyOpen := bodyFor(toks, paramsClose+1, close, int(t.Line))
		if bodyOpen < 0 {
			k = paramsClose
			continue
		}
		bodyEnd := matchBlock(toks, bodyOpen)
		if bodyEnd <= bodyOpen {
			k = bodyOpen
			continue
		}
		line := int(t.Line)
		mid := MakeID(cid, name)
		b.AddNode(mid, "."+name+"()", line)
		b.Edge(cid, mid, "method", line)
		*spans = append(*spans, FuncSpan{ID: mid, Start: bodyOpen, End: bodyEnd})
		k = bodyEnd
	}
}
