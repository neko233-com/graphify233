package extract

import (
	"os"
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractGo walks top-level declarations: funcs, methods (attached to their
// receiver type), type specs and imports. Call edges are inferred afterwards
// from the bodies, exactly like the reference tree-sitter walker.
func extractGo(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.CLike)

	depth := 0
	var spans []FuncSpan
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind == lex.Punct {
			switch t.Text {
			case "{":
				depth++
			case "}":
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
		case "func":
			i = goFunc(b, src, toks, i, &spans)
		case "type":
			i = goType(b, toks, i)
		case "import":
			i = goImport(b, src, toks, i)
		}
	}

	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func goFunc(b *Builder, src string, toks []lex.Token, i int, spans *[]FuncSpan) int {
	j := i + 1
	parentID, parentLabel := "", ""
	if j < len(toks) && isPunct(toks[j], "(") {
		close := matchBlock(toks, j)
		if close < 0 {
			return i
		}
		// Receiver type: the identifier list at depth 0 holds [name, Type]
		// (name optional); generics keep their brackets.
		var idents []int
		d := 0
		for k := j + 1; k < close; k++ {
			if toks[k].Kind == lex.Punct {
				switch toks[k].Text {
				case "(", "[", "{":
					d++
				case ")", "]", "}":
					if d > 0 {
						d--
					}
				}
				continue
			}
			if d == 0 && toks[k].Kind == lex.Ident {
				idents = append(idents, k)
			}
		}
		if len(idents) > 0 {
			first := idents[0]
			if len(idents) > 1 {
				first = idents[1]
			}
			raw := strings.TrimSpace(src[toks[first].Start:toks[close].Start])
			raw = strings.TrimSpace(strings.TrimLeft(raw, "*"))
			raw = strings.TrimSpace(strings.TrimSuffix(raw, ","))
			parentLabel = raw
			parentID = MakeID(b.Stem, raw)
		}
		j = close + 1
	}
	if j >= len(toks) || toks[j].Kind != lex.Ident {
		return i
	}
	name := toks[j].Text
	line := int(toks[j].Line)
	var fid, label string
	if parentID != "" {
		b.AddNode(parentID, parentLabel, line)
		fid = MakeID(parentID, name)
		label = "." + name + "()"
		b.AddNode(fid, label, line)
		b.Edge(parentID, fid, "method", line)
	} else {
		fid = MakeID(b.Stem, name)
		label = name + "()"
		b.AddNode(fid, label, line)
		b.Edge(b.FileID, fid, "contains", line)
	}
	if open := findBody(toks, j+1); open >= 0 {
		if close := matchBlock(toks, open); close > open {
			*spans = append(*spans, FuncSpan{ID: fid, Start: open, End: close})
			return close
		}
	}
	return j
}

func goType(b *Builder, toks []lex.Token, i int) int {
	j := i + 1
	if j >= len(toks) {
		return i
	}
	if isPunct(toks[j], "(") {
		close := matchBlock(toks, j)
		if close < 0 {
			return i
		}
		d := 0
		for k := j + 1; k < close; k++ {
			switch {
			case toks[k].Kind == lex.Punct && (toks[k].Text == "(" || toks[k].Text == "[" || toks[k].Text == "{"):
				d++
			case toks[k].Kind == lex.Punct && (toks[k].Text == ")" || toks[k].Text == "]" || toks[k].Text == "}"):
				if d > 0 {
					d--
				}
			case d == 0 && toks[k].Kind == lex.Ident:
				if k == j+1 || toks[k-1].Line < toks[k].Line {
					nid := MakeID(b.Stem, toks[k].Text)
					b.AddNode(nid, toks[k].Text, int(toks[k].Line))
					b.Edge(b.FileID, nid, "contains", int(toks[k].Line))
				}
			}
		}
		return close
	}
	if toks[j].Kind == lex.Ident {
		nid := MakeID(b.Stem, toks[j].Text)
		b.AddNode(nid, toks[j].Text, int(toks[j].Line))
		b.Edge(b.FileID, nid, "contains", int(toks[j].Line))
		return j
	}
	return i
}

func goImport(b *Builder, src string, toks []lex.Token, i int) int {
	j := i + 1
	if j >= len(toks) {
		return i
	}
	add := func(line int, path string) {
		path = strings.Trim(path, "\"`")
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		mod := path
		if k := strings.LastIndexByte(mod, '/'); k >= 0 {
			mod = mod[k+1:]
		}
		b.Edge(b.FileID, MakeID(mod), "imports_from", line)
	}
	if isPunct(toks[j], "(") {
		close := matchBlock(toks, j)
		if close < 0 {
			return i
		}
		for k := j + 1; k < close; k++ {
			if toks[k].Kind == lex.String {
				add(int(toks[k].Line), toks[k].Text)
			}
		}
		return close
	}
	if toks[j].Kind == lex.String {
		add(int(toks[j].Line), toks[j].Text)
		return j
	}
	return i
}
