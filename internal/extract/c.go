package extract

import (
	"os"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractC scans C source: #include directives become imports edges, and
// top-level function definitions become nodes with their bodies scanned for
// calls. Struct declarations are ignored, as in the reference implementation.
func extractC(path, stem, ext string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	includeHeader(b, src)
	toks := lex.Tokenize(src, lex.CLike)
	var spans []FuncSpan
	cFuncs(b, toks, 0, len(toks), "", &spans)
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

// cFuncs finds function definitions in [start,end) at brace depth 0.
func cFuncs(b *Builder, toks []lex.Token, start, end int, parentID string, spans *[]FuncSpan) {
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
		if depth != 0 || t.Kind != lex.Ident || cReserved[t.Text] {
			continue
		}
		if i+1 >= end || !isPunct(toks[i+1], "(") {
			continue
		}
		paramsClose := matchBlock(toks, i+1)
		if paramsClose < 0 || paramsClose+1 >= end || !isPunct(toks[paramsClose+1], "{") {
			continue
		}
		line := int(t.Line)
		name := t.Text
		var id string
		if parentID != "" {
			id = MakeID(parentID, name)
			b.AddNode(id, "."+name+"()", line)
			b.Edge(parentID, id, "method", line)
		} else {
			id = MakeID(b.Stem, name)
			b.AddNode(id, name+"()", line)
			b.Edge(b.FileID, id, "contains", line)
		}
		if bodyEnd := matchBlock(toks, paramsClose+1); bodyEnd > paramsClose {
			*spans = append(*spans, FuncSpan{ID: id, Start: paramsClose + 1, End: bodyEnd})
			i = bodyEnd
		}
	}
}

// cReserved holds keywords that can never be a function name.
var cReserved = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "sizeof": true,
	"return": true, "do": true, "else": true, "case": true, "goto": true,
	"typedef": true, "struct": true, "union": true, "enum": true, "static": true,
	"extern": true, "const": true, "unsigned": true, "signed": true, "volatile": true,
	"register": true, "inline": true, "break": true, "continue": true, "default": true,
	"void": true, "int": true, "char": true, "float": true, "double": true,
	"long": true, "short": true, "auto": true, "restrict": true, "define": true,
	"_Generic": true, "alignof": true, "_Alignof": true, "typeof": true,
	"class": true, "namespace": true, "using": true, "template": true, "typename": true,
	"public": true, "private": true, "protected": true, "virtual": true,
	"operator": true, "new": true, "delete": true, "this": true, "try": true,
	"catch": true, "throw": true, "noexcept": true, "constexpr": true, "nullptr": true,
	"bool": true, "true": true, "false": true, "explicit": true, "friend": true,
}
