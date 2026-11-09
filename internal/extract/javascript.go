package extract

import (
	"os"
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractJS handles .js/.jsx/.ts/.tsx: top-level classes with their methods,
// function declarations, arrow functions bound with const/let/var, and
// import statements. Declarations nested inside other functions are skipped,
// matching the reference's single-scope behaviour.
func extractJS(path, stem, ext string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	toks := lex.Tokenize(src, lex.JS())

	var spans []FuncSpan
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
		if depth != 0 || t.Kind != lex.Ident {
			continue
		}
		switch t.Text {
		case "class":
			i = jsClass(b, toks, i, &spans)
		case "function":
			i = jsFunction(b, toks, i, &spans)
		case "import":
			i = jsImport(b, toks, i)
		case "const", "let", "var":
			i = jsLexical(b, toks, i, &spans)
		}
	}
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func jsClassName(toks []lex.Token, i int) (string, int) {
	if i+1 >= len(toks) || toks[i+1].Kind != lex.Ident {
		return "", i
	}
	name := toks[i+1].Text
	if name == "extends" || name == "implements" {
		return "", i
	}
	return name, i + 1
}

func jsClass(b *Builder, toks []lex.Token, i int, spans *[]FuncSpan) int {
	name, _ := jsClassName(toks, i)
	if name == "" {
		return i
	}
	line := int(toks[i].Line)
	nid := MakeID(b.Stem, name)
	b.AddNode(nid, name, line)
	b.Edge(b.FileID, nid, "contains", line)

	open := findBody(toks, i+2)
	if open < 0 {
		return i
	}
	close := matchBlock(toks, open)
	if close < 0 {
		return i
	}
	jsMethods(b, toks, open, close, nid, spans)
	return close
}

func jsMethods(b *Builder, toks []lex.Token, open, limit int, classID string, spans *[]FuncSpan) {
	d := 0
	for k := open + 1; k < limit; k++ {
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
		if d != 0 || t.Kind != lex.Ident || !jsMethodStart(toks, k) {
			continue
		}
		// Locate the parameter list; generic methods put <T> first.
		params := k + 1
		if isPunct(toks[params], "<") {
			params = jsSkipGenerics(toks, params)
			if params < 0 {
				continue
			}
		}
		if params >= limit || !isPunct(toks[params], "(") {
			continue
		}
		endParams := matchBlock(toks, params)
		if endParams < 0 {
			continue
		}
		line := int(t.Line)
		methodID := MakeID(classID, t.Text)
		b.AddNode(methodID, "."+t.Text+"()", line)
		b.Edge(classID, methodID, "method", line)
		if body := findBody(toks, endParams+1); body >= 0 && body < limit {
			if end := matchBlock(toks, body); end > body {
				*spans = append(*spans, FuncSpan{ID: methodID, Start: body, End: end})
				k = end
				continue
			}
		}
		k = endParams
	}
}

// jsMethodStart rejects identifiers that are values rather than method names.
func jsMethodStart(toks []lex.Token, i int) bool {
	if i+1 >= len(toks) {
		return false
	}
	next := toks[i+1]
	if !(isPunct(next, "(") || isPunct(next, "<")) {
		return false
	}
	if i == 0 {
		return true
	}
	prev := toks[i-1]
	if prev.Kind == lex.Punct {
		switch prev.Text {
		case "{", "}", ";", "*", "]", "@":
			return true
		}
		switch prev.Text {
		case "=", ",", "(", "=>":
			return false
		}
		return false
	}
	if prev.Kind == lex.Ident {
		switch prev.Text {
		case "get", "set", "async", "static", "public", "private", "protected",
			"readonly", "abstract", "override", "declare", "export", "default":
			return true
		}
		return false
	}
	return false
}

func jsSkipGenerics(toks []lex.Token, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		if toks[i].Kind != lex.Punct {
			continue
		}
		switch toks[i].Text {
		case "<":
			depth++
		case ">":
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func jsFunction(b *Builder, toks []lex.Token, i int, spans *[]FuncSpan) int {
	j := i + 1
	for j < len(toks) && isPunct(toks[j], "*") {
		j++
	}
	if j >= len(toks) || toks[j].Kind != lex.Ident {
		return i
	}
	name := toks[j].Text
	line := int(toks[i].Line)
	nid := MakeID(b.Stem, name)
	b.AddNode(nid, name+"()", line)
	b.Edge(b.FileID, nid, "contains", line)
	if body := findBody(toks, j+1); body >= 0 {
		if end := matchBlock(toks, body); end > body {
			*spans = append(*spans, FuncSpan{ID: nid, Start: body, End: end})
			return end
		}
	}
	return j
}

func jsImport(b *Builder, toks []lex.Token, i int) int {
	line := int(toks[i].Line)
	j := i + 1
	if j < len(toks) && isPunct(toks[j], "(") {
		return i // dynamic import
	}
	for k := j; k < len(toks) && k < j+200; k++ {
		t := toks[k]
		if isPunct(t, ";") {
			return k
		}
		if t.Kind == lex.String {
			raw := strings.Trim(t.Text, "'\"` ")
			module := strings.TrimLeft(raw, "./")
			if idx := strings.LastIndexByte(module, '/'); idx >= 0 {
				module = module[idx+1:]
			}
			if module != "" {
				b.Edge(b.FileID, MakeID(module), "imports_from", line)
			}
			return k
		}
	}
	return i
}

func jsLexical(b *Builder, toks []lex.Token, i int, spans *[]FuncSpan) int {
	j := i + 1
	for j < len(toks) {
		if toks[j].Kind != lex.Ident {
			break
		}
		name := toks[j].Text
		line := int(toks[j].Line)
		k := jsSkipType(toks, j+1)
		if k < 0 || k >= len(toks) || !isPunct(toks[k], "=") {
			return j
		}
		v := k + 1
		if v >= len(toks) {
			return v
		}
		// const foo = (a: T) => {...} | x => expr
		arrow := -1
		if isPunct(toks[v], "(") {
			if p := matchBlock(toks, v); p > 0 && p+1 < len(toks) && isPunct(toks[p+1], "=>") {
				arrow = p + 1
			}
		} else if toks[v].Kind == lex.Ident && v+1 < len(toks) && isPunct(toks[v+1], "=>") {
			arrow = v + 1
		}
		if arrow < 0 {
			return j
		}
		nid := MakeID(b.Stem, name)
		b.AddNode(nid, name+"()", line)
		b.Edge(b.FileID, nid, "contains", line)
		if arrow+1 < len(toks) && isPunct(toks[arrow+1], "{") {
			if end := matchBlock(toks, arrow+1); end > arrow+1 {
				*spans = append(*spans, FuncSpan{ID: nid, Start: arrow + 1, End: end})
				return end
			}
		}
		// Expression body: runs until ',' or ';' at depth 0.
		end := arrow + 1
		d := 0
		for ; end < len(toks); end++ {
			if toks[end].Kind != lex.Punct {
				continue
			}
			switch toks[end].Text {
			case "{", "(", "[":
				d++
			case "}", ")", "]":
				if d > 0 {
					d--
				}
			case ",", ";":
				if d == 0 {
					*spans = append(*spans, FuncSpan{ID: nid, Start: arrow, End: end})
					return end - 1
				}
			}
		}
		*spans = append(*spans, FuncSpan{ID: nid, Start: arrow, End: end})
		return end
	}
	return i
}

// jsSkipType skips a TypeScript type annotation and returns the index of '='.
func jsSkipType(toks []lex.Token, i int) int {
	if i < len(toks) && isPunct(toks[i], ":") {
		d := 0
		for j := i + 1; j < len(toks); j++ {
			if toks[j].Kind != lex.Punct {
				continue
			}
			switch toks[j].Text {
			case "<", "(", "[", "{":
				d++
			case ">", ")", "]", "}":
				if d > 0 {
					d--
				}
			case "=":
				if d == 0 {
					return j
				}
			case ";":
				return -1
			}
		}
		return -1
	}
	return i
}
