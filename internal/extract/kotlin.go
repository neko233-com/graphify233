package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractKotlin scans class/interface/object declarations and their fun
// members. Imports and calls are not extracted: the reference Kotlin extractor
// only produces structural nodes and method edges.
func extractKotlin(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	toks := lex.Tokenize(string(data), lex.CLike)
	ktDecls(b, toks, 0, len(toks), "", "")
	return b.Result()
}

func ktDecls(b *Builder, toks []lex.Token, start, end int, cid, cname string) {
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
		case "import", "package":
			i = lineEnd(toks, i, end)
		case "class", "interface", "object":
			if i+1 >= end || toks[i+1].Kind != lex.Ident || toks[i+1].Text == "class" {
				continue
			}
			name := toks[i+1].Text
			line := int(t.Line)
			nid := MakeID(b.Stem, name)
			b.AddNode(nid, name, line)
			b.Edge(b.FileID, nid, "contains", line)
			open := bodyFor(toks, i+2, end, line)
			if open < 0 {
				continue
			}
			close := matchBlock(toks, open)
			if close > open {
				ktDecls(b, toks, open+1, close, nid, name)
				i = close
			}
		case "fun":
			name, nameIdx := ktFuncName(toks, i, end)
			if name == "" {
				continue
			}
			line := int(toks[nameIdx].Line)
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
			params := nameIdx + 1
			if params < end && isPunct(toks[params], "(") {
				paramsClose := matchBlock(toks, params)
				if paramsClose > params {
					if bodyOpen := bodyFor(toks, paramsClose+1, end, line); bodyOpen >= 0 {
						if bodyEnd := matchBlock(toks, bodyOpen); bodyEnd > bodyOpen {
							i = bodyEnd
							continue
						}
					}
					i = paramsClose
				}
			}
		}
	}
}

func ktFuncName(toks []lex.Token, i, end int) (string, int) {
	limit := i + 24
	if limit > end {
		limit = end
	}
	for j := i + 1; j < limit; j++ {
		if toks[j].Kind == lex.Ident && j+1 < end && isPunct(toks[j+1], "(") {
			return toks[j].Text, j
		}
		if isPunct(toks[j], "{") || isPunct(toks[j], "}") || isPunct(toks[j], ";") {
			return "", -1
		}
	}
	return "", -1
}

// lineEnd advances i to the last token on the same source line.
func lineEnd(toks []lex.Token, i, end int) int {
	for i+1 < end && toks[i+1].Line == toks[i].Line {
		i++
	}
	return i
}
