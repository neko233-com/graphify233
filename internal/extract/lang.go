package extract

import (
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
)

// includeHeader records `#include <x>` / `#include "x"` lines as imports edges
// to the header's base name, matching the reference C/C++ extractor.
func includeHeader(b *Builder, src string) {
	line := 0
	for _, raw := range strings.Split(src, "\n") {
		line++
		s := strings.TrimSpace(raw)
		if !strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimSpace(s[1:])
		if !strings.HasPrefix(s, "include") {
			continue
		}
		s = strings.TrimSpace(s[len("include"):])
		var name string
		if strings.HasPrefix(s, "<") {
			if k := strings.IndexByte(s, '>'); k > 1 {
				name = s[1:k]
			}
		} else if strings.HasPrefix(s, "\"") {
			if k := strings.IndexByte(s[1:], '"'); k > 0 {
				name = s[1 : 1+k]
			}
		}
		if name == "" {
			continue
		}
		if k := strings.LastIndexAny(name, "/\\"); k >= 0 {
			name = name[k+1:]
		}
		if k := strings.LastIndexByte(name, '.'); k > 0 {
			name = name[:k]
		}
		if name != "" {
			b.Edge(b.FileID, MakeID(name), "imports", line)
		}
	}
}

// declStops are declaration keywords that end a previous declaration's reach:
// finding one at brace depth 0 before a body brace means the declaration has
// no body (an interface method, a data class, ...).
var declStops = map[string]bool{
	"class": true, "def": true, "fun": true, "function": true, "namespace": true,
	"object": true, "trait": true, "interface": true, "impl": true, "package": true,
	"import": true, "use": true, "using": true, "struct": true, "enum": true,
	"public": true, "private": true, "protected": true, "internal": true,
	"val": true, "var": true,
}

// bodyFor finds the opening brace of a declaration body. It stops when a new
// declaration keyword appears on a later line, so a bodyless declaration does
// not swallow the next one's body.
func bodyFor(toks []lex.Token, from, limit, declLine int) int {
	depth := 0
	for j := from; j < limit; j++ {
		t := toks[j]
		if t.Kind == lex.Punct {
			switch t.Text {
			case "(", "[":
				depth++
			case ")", "]":
				if depth > 0 {
					depth--
				}
			case "{":
				if depth == 0 {
					return j
				}
			case ";", "}":
				if depth == 0 {
					return -1
				}
			}
			continue
		}
		if depth == 0 && t.Kind == lex.Ident && declStops[t.Text] && int(t.Line) > declLine {
			return -1
		}
	}
	return -1
}

// stmtEnd returns the index of the next ';' in [from,limit), or limit.
func stmtEnd(toks []lex.Token, from, limit int) int {
	for j := from; j < limit; j++ {
		if isPunct(toks[j], ";") {
			return j
		}
	}
	return limit
}
