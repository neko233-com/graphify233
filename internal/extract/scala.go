package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractScala scans imports (first path segment), classes/traits/objects and
// their def members. Def bodies are scanned for calls.
func extractScala(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	toks := lex.Tokenize(string(data), lex.CLike)
	var spans []FuncSpan
	scalaDecls(b, toks, 0, len(toks), "", "", &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func scalaDecls(b *Builder, toks []lex.Token, start, end int, cid, cname string, spans *[]FuncSpan) {
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
		case "import":
			line := int(t.Line)
			if i+1 < end && toks[i+1].Kind == lex.Ident {
				// Scala imports resolve the package root: `import a.b.C` → a.
				b.Edge(b.FileID, MakeID(toks[i+1].Text), "imports", line)
			}
			i = lineEnd(toks, i, end)
		case "case":
			// `case class` / `case object`: the following keyword handles it.
			if i+1 < end && (toks[i+1].Text == "class" || toks[i+1].Text == "object") {
				continue
			}
		case "class", "trait", "object":
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
				scalaDecls(b, toks, open+1, close, nid, name, spans)
				i = close
			}
		case "def":
			i = scalaDef(b, toks, i, end, cid, spans)
		}
	}
}

func scalaDef(b *Builder, toks []lex.Token, i, end int, cid string, spans *[]FuncSpan) int {
	if i+1 >= end || toks[i+1].Kind != lex.Ident {
		return i
	}
	name := toks[i+1].Text
	line := int(toks[i].Line)
	j := i + 2
	if j < end && isPunct(toks[j], "[") {
		if close := matchBlock(toks, j); close > j {
			j = close + 1
		}
	}
	if j >= end || !isPunct(toks[j], "(") {
		return i
	}
	paramsClose := matchBlock(toks, j)
	if paramsClose < 0 {
		return i
	}
	bodyOpen, eq := -1, -1
	after := paramsClose
	for k := paramsClose + 1; k < end; k++ {
		tk := toks[k]
		if tk.Kind == lex.Punct {
			switch tk.Text {
			case "{":
				bodyOpen = k
			case "=":
				if eq < 0 {
					eq = k
				}
			case "}", ";":
				after = k
				goto done
			}
			if bodyOpen >= 0 {
				break
			}
			continue
		}
		if tk.Kind == lex.Ident && declStops[tk.Text] && int(tk.Line) > line {
			after = k
			break
		}
	}
done:

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

	if bodyOpen >= 0 {
		if bodyEnd := matchBlock(toks, bodyOpen); bodyEnd > bodyOpen {
			*spans = append(*spans, FuncSpan{ID: fid, Start: bodyOpen, End: bodyEnd})
			return bodyEnd
		}
		return after
	}
	if eq >= 0 {
		// Expression body: calls up to the end of the closing line.
		last := eq
		for last+1 < end && toks[last+1].Line == toks[eq].Line {
			last++
		}
		*spans = append(*spans, FuncSpan{ID: fid, Start: eq, End: last + 1})
		return last
	}
	return after
}
