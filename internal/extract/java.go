package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractJava scans Java sources: imports become imports edges to the last
// dotted segment, classes/interfaces/enums become nodes, and their methods
// (including constructors and interface method signatures) become methods.
func extractJava(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.CLike)
	javaImports(b, toks)
	var spans []FuncSpan
	javaDecls(b, toks, 0, len(toks), &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func javaImports(b *Builder, toks []lex.Token) {
	depth := 0
	for i := 0; i < len(toks); i++ {
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
		if depth != 0 || t.Kind != lex.Ident || t.Text != "import" {
			continue
		}
		line := int(t.Line)
		last := ""
		j := i + 1
		for ; j < len(toks); j++ {
			if isPunct(toks[j], ";") {
				break
			}
			if toks[j].Kind == lex.Ident {
				last = toks[j].Text
			}
		}
		if last != "" {
			b.Edge(b.FileID, MakeID(last), "imports", line)
		}
		i = j
	}
}

func javaDecls(b *Builder, toks []lex.Token, start, end int, spans *[]FuncSpan) {
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
		case "class", "interface", "enum", "record":
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
			javaMembers(b, toks, open, close, nid, name, spans)
			i = close
		}
	}
}

func javaMembers(b *Builder, toks []lex.Token, open, close int, cid, cname string, spans *[]FuncSpan) {
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
		case "class", "interface", "enum", "record":
			if k+1 < close && toks[k+1].Kind == lex.Ident {
				name := toks[k+1].Text
				line := int(t.Line)
				nid := MakeID(b.Stem, name)
				b.AddNode(nid, name, line)
				b.Edge(b.FileID, nid, "contains", line)
				if inner := bodyFor(toks, k+2, close, line); inner >= 0 {
					if innerClose := matchBlock(toks, inner); innerClose > inner {
						javaMembers(b, toks, inner, innerClose, nid, name, spans)
						k = innerClose
					}
				}
			}
			continue
		}
		if k+1 >= close || !isPunct(toks[k+1], "(") || !javaCallable(toks, k, open) {
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

// javaCallable reports whether the identifier at k is declared as a method:
// it must follow a type or modifier, never a construction or a member access.
func javaCallable(toks []lex.Token, k, open int) bool {
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
		case "new", "return", "throw", "else", "this", "super":
			return false
		}
		return true
	}
	return false
}
