package extract

import (
	"os"
	"strings"

	"github.com/neko233-com/graphify233/internal/lex"
	"github.com/neko233-com/graphify233/internal/model"
)

// extractRuby scans classes/modules and their defs using indentation and block
// matching. Method bodies are scanned for calls; `require` lines do not produce
// edges, matching the reference extractor.
func extractRuby(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	lines := splitLines(src)

	type frame struct {
		indent int
		id     string
	}
	type rbFunc struct {
		id        string
		startLine int
		endLine   int
	}
	var stack []frame
	var funcs []rbFunc

	for idx := 0; idx < len(lines); idx++ {
		raw := lines[idx]
		lineNo := idx + 1
		code := strings.TrimSpace(stripRubyComment(raw))
		if code == "" {
			continue
		}
		indent := rubyIndent(raw)
		for len(stack) > 0 && code == "end" && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		switch {
		case strings.HasPrefix(code, "class ") || strings.HasPrefix(code, "class <<"):
			name := rubyClassName(code)
			if name == "" {
				continue
			}
			nid := MakeID(b.Stem, name)
			b.AddNode(nid, name, lineNo)
			b.Edge(b.FileID, nid, "contains", lineNo)
			stack = append(stack, frame{indent: indent, id: nid})
		case strings.HasPrefix(code, "module "):
			name := rubyClassName(code)
			if name == "" {
				continue
			}
			nid := MakeID(b.Stem, name)
			b.AddNode(nid, name, lineNo)
			b.Edge(b.FileID, nid, "contains", lineNo)
			stack = append(stack, frame{indent: indent, id: nid})
		case strings.HasPrefix(code, "def "):
			name := rubyDefName(code)
			if name == "" {
				continue
			}
			var fid string
			if len(stack) > 0 && indent > stack[len(stack)-1].indent {
				owner := stack[len(stack)-1].id
				fid = MakeID(owner, name)
				b.AddNode(fid, "."+name+"()", lineNo)
				b.Edge(owner, fid, "method", lineNo)
			} else {
				fid = MakeID(b.Stem, name)
				b.AddNode(fid, name+"()", lineNo)
				b.Edge(b.FileID, fid, "contains", lineNo)
			}
			endIdx := rubyBlockEnd(lines, idx)
			if endIdx < idx {
				endIdx = idx
			}
			funcs = append(funcs, rbFunc{id: fid, startLine: lineNo, endLine: endIdx + 1})
			idx = endIdx
		}
	}

	toks := lex.Tokenize(src, lex.Hash())
	var spans []FuncSpan
	for _, f := range funcs {
		startTok, endTok := -1, -1
		for ti := 0; ti < len(toks); ti++ {
			ln := int(toks[ti].Line)
			if startTok < 0 && ln >= f.startLine {
				startTok = ti
			}
			if ln <= f.endLine {
				endTok = ti
			}
		}
		if startTok < 0 || endTok <= startTok {
			continue
		}
		start := startTok - 1
		if start < 0 {
			start = 0
		}
		spans = append(spans, FuncSpan{ID: f.id, Start: start, End: endTok + 1})
	}
	ScanCalls(toks, spans, b, nil)
	return b.Result()
}

func stripRubyComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			return line[:i]
		}
	}
	return line
}

func rubyIndent(line string) int {
	n := 0
	for _, c := range line {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 2
		default:
			return n
		}
	}
	return n
}

func rubyClassName(code string) string {
	rest := strings.TrimSpace(code[strings.IndexByte(code, ' ')+1:])
	if k := strings.IndexAny(rest, " <\t"); k > 0 {
		rest = rest[:k]
	}
	return strings.TrimSpace(rest)
}

func rubyDefName(code string) string {
	rest := strings.TrimSpace(code[len("def "):])
	if k := strings.IndexAny(rest, "( \t"); k > 0 {
		rest = rest[:k]
	}
	if k := strings.LastIndexByte(rest, '.'); k >= 0 {
		rest = rest[k+1:]
	}
	return strings.TrimSpace(rest)
}

// rubyBlockEnd finds the `end` line matching the block opened at start.
func rubyBlockEnd(lines []string, start int) int {
	depth := 1
	for i := start + 1; i < len(lines); i++ {
		code := strings.TrimSpace(stripRubyComment(lines[i]))
		if code == "" {
			continue
		}
		if strings.HasPrefix(code, "end") {
			depth--
			if depth == 0 {
				return i
			}
			continue
		}
		if rubyOpensBlock(code) {
			depth++
		}
	}
	return start
}

func rubyOpensBlock(code string) bool {
	first := code
	if k := strings.IndexAny(first, " ("); k > 0 {
		first = first[:k]
	}
	switch first {
	case "def", "if", "unless", "while", "until", "for", "case", "begin",
		"class", "module", "do":
		return true
	}
	if strings.HasSuffix(code, " do") || strings.Contains(code, " do |") {
		return true
	}
	return false
}
