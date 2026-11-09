package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractCSharp scans C# sources: using directives become imports edges to the
// last dotted segment, namespaces and classes/interfaces become nodes, and
// class members become methods. Members of a namespace are attached to the
// file, matching the reference extractor.
func extractCSharp(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.CLike)
	var spans []FuncSpan
	csDecls(b, toks, 0, len(toks), &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func csDecls(b *Builder, toks []lex.Token, start, end int, spans *[]FuncSpan) {
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
		case "using":
			if i+1 < end && toks[i+1].Kind == lex.Ident {
				line := int(t.Line)
				last := ""
				j := i + 1
				for ; j < end; j++ {
					if isPunct(toks[j], ";") {
						break
					}
					if toks[j].Kind == lex.Ident {
						last = toks[j].Text
					}
				}
				if last != "" && last != "var" {
					b.Edge(b.FileID, MakeID(last), "imports", line)
				}
				i = j
			}
		case "namespace":
			if i+1 < end && toks[i+1].Kind == lex.Ident {
				name := toks[i+1].Text
				line := int(t.Line)
				nid := MakeID(b.Stem, name)
				b.AddNode(nid, name, line)
				b.Edge(b.FileID, nid, "contains", line)
				if open := bodyFor(toks, i+2, end, line); open >= 0 {
					if close := matchBlock(toks, open); close > open {
						csDecls(b, toks, open+1, close, spans)
						i = close
					}
				}
			}
		case "class", "interface", "struct", "enum", "record":
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
			if close <= open {
				continue
			}
			csMembers(b, toks, open, close, nid, name, spans)
			i = close
		}
	}
}

func csMembers(b *Builder, toks []lex.Token, open, close int, cid, cname string, spans *[]FuncSpan) {
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
		if d != 0 || t.Kind != lex.Ident {
			continue
		}
		switch t.Text {
		case "class", "interface", "struct", "enum", "record":
			if k+1 < close && toks[k+1].Kind == lex.Ident {
				name := toks[k+1].Text
				line := int(t.Line)
				nid := MakeID(b.Stem, name)
				b.AddNode(nid, name, line)
				b.Edge(b.FileID, nid, "contains", line)
				if inner := bodyFor(toks, k+2, close, line); inner >= 0 {
					if innerClose := matchBlock(toks, inner); innerClose > inner {
						csMembers(b, toks, inner, innerClose, nid, name, spans)
						k = innerClose
					}
				}
			}
			continue
		}
		if k+1 >= close || !isPunct(toks[k+1], "(") || !csCallable(toks, k, open) {
			continue
		}
		// Constructors are constructor_declaration nodes in C#, not methods.
		if t.Text == cname {
			continue
		}
		paramsClose := matchBlock(toks, k+1)
		if paramsClose < 0 || paramsClose >= close {
			continue
		}
		line := int(t.Line)
		mid := MakeID(cid, t.Text)
		b.AddNode(mid, "."+t.Text+"()", line)
		b.Edge(cid, mid, "method", line)
		if isPunct(toks[paramsClose+1], ";") {
			k = paramsClose + 1
			continue
		}
		bodyOpen := bodyFor(toks, paramsClose+1, close, line)
		if bodyOpen < 0 {
			k = paramsClose
			continue
		}
		bodyEnd := matchBlock(toks, bodyOpen)
		if bodyEnd <= bodyOpen {
			k = bodyOpen
			continue
		}
		*spans = append(*spans, FuncSpan{ID: mid, Start: bodyOpen, End: bodyEnd})
		k = bodyEnd
	}
}

func csCallable(toks []lex.Token, k, open int) bool {
	if k <= open {
		return false
	}
	prev := toks[k-1]
	switch prev.Kind {
	case lex.Punct:
		switch prev.Text {
		case ">", "]", "?":
			return true
		}
		return false
	case lex.Ident:
		switch prev.Text {
		case "new", "return", "throw", "else", "this", "nameof", "typeof", "sizeof":
			return false
		}
		return true
	}
	return false
}
