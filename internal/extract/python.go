package extract

import (
	"os"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

// Python is parsed with a lightweight indentation-aware scanner rather than a
// full grammar. It recognises classes, methods, functions, imports and calls,
// which is the structural surface the reference extracts.
func extractPython(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypeCode)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	src := string(data)
	lines := splitLines(src)

	// First pass: collect all class names so forward references resolve to the
	// real node instead of an external stub.
	classNames := make(map[string]bool)
	for _, l := range lines {
		if t := strings.TrimSpace(stripPyComment(l)); strings.HasPrefix(t, "class ") {
			if name, _ := parsePyClassName(t); name != "" {
				classNames[name] = true
			}
		}
	}

	masker := &pyMasker{}
	masks := make([]string, len(lines))
	for i, l := range lines {
		masks[i] = masker.mask(l)
	}

	const (
		scopeModule = iota
		scopeClass
		scopeFunc
		scopeIgnored
	)
	type span struct {
		id    string
		start int
		end   int
	}
	type frame struct {
		indent int
		kind   int
		id     string
		span   *span
		ignore *[2]int
	}
	root := frame{indent: -1, kind: scopeModule}
	frames := []frame{root}
	var funcs []*span
	var ignored [][2]int

	pop := func(indent, lineIdx int) {
		for len(frames) > 1 && indent <= frames[len(frames)-1].indent {
			f := frames[len(frames)-1]
			frames = frames[:len(frames)-1]
			if f.span != nil && f.span.end < 0 {
				f.span.end = lineIdx - 1
			}
			if f.ignore != nil && f.ignore[1] < 0 {
				f.ignore[1] = lineIdx - 1
				ignored = append(ignored, *f.ignore)
			}
		}
	}

	for i, line := range lines {
		masked := masks[i]
		trimmed := strings.TrimSpace(masked)
		if trimmed == "" {
			continue
		}
		indent := pyIndent(line)
		pop(indent, i)
		parent := frames[len(frames)-1]

		switch {
		case strings.HasPrefix(trimmed, "class "):
			name, bases := parsePyClassName(trimmed)
			if name == "" {
				break
			}
			if parent.kind != scopeModule && parent.kind != scopeClass {
				r := [2]int{i, -1}
				frames = append(frames, frame{indent: indent, kind: scopeIgnored, ignore: &r})
				break
			}
			nid := MakeID(stem, name)
			b.AddNode(nid, name, i+1)
			b.Edge(b.FileID, nid, "contains", i+1)
			for _, base := range bases {
				baseID := MakeID(base)
				if classNames[base] {
					baseID = MakeID(stem, base)
				} else {
					b.AddStub(baseID, base)
				}
				b.Edge(nid, baseID, "inherits", i+1)
			}
			frames = append(frames, frame{indent: indent, kind: scopeClass, id: nid})

		case strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "async def "):
			name := parsePyFuncName(trimmed)
			if name == "" {
				break
			}
			if parent.kind != scopeModule && parent.kind != scopeClass {
				r := [2]int{i, -1}
				frames = append(frames, frame{indent: indent, kind: scopeIgnored, ignore: &r})
				break
			}
			var fid, label string
			if parent.kind == scopeClass {
				fid = MakeID(parent.id, name)
				label = "." + name + "()"
				b.AddNode(fid, label, i+1)
				b.Edge(parent.id, fid, "method", i+1)
			} else {
				fid = MakeID(stem, name)
				label = name + "()"
				b.AddNode(fid, label, i+1)
				b.Edge(b.FileID, fid, "contains", i+1)
			}
			sp := &span{id: fid, start: i, end: -1}
			funcs = append(funcs, sp)
			frames = append(frames, frame{indent: indent, kind: scopeFunc, id: fid, span: sp})

		default:
			if parent.kind == scopeModule || parent.kind == scopeClass {
				parsePyImport(b, trimmed, i+1)
			}
		}
	}
	pop(-1, len(lines)) // closes anything still open at EOF

	if len(ignored) > 0 {
		sort.Slice(ignored, func(i, j int) bool { return ignored[i][0] < ignored[j][0] })
	}

	for _, sp := range funcs {
		if sp.end < 0 || sp.end >= len(lines) {
			sp.end = len(lines) - 1
		}
		for i := sp.start + 1; i <= sp.end; i++ {
			if inRange(ignored, i) || strings.TrimSpace(masks[i]) == "" {
				continue
			}
			scanPyCalls(b, sp.id, masks[i], i+1)
		}
	}

	return b.Result()
}

func inRange(ranges [][2]int, line int) bool {
	for _, r := range ranges {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

func splitLines(src string) []string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// pyMasker blanks comments and string literals so structural scans never
// mistake their contents for code. Triple-quoted strings carry across lines.
type pyMasker struct {
	triple byte
}

func stripPyComment(line string) string {
	inS, inD := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' {
			i++
			continue
		}
		switch {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '#' && !inS && !inD:
			return line[:i]
		}
	}
	return line
}

func (m *pyMasker) mask(line string) string {
	buf := []byte(line)
	i := 0
	for i < len(buf) {
		if m.triple != 0 {
			closing := strings.Repeat(string(m.triple), 3)
			end := strings.Index(line[i:], closing)
			if end < 0 {
				for j := i; j < len(buf); j++ {
					buf[j] = ' '
				}
				return string(buf)
			}
			for j := i; j < i+end+3; j++ {
				buf[j] = ' '
			}
			i += end + 3
			m.triple = 0
			continue
		}
		c := buf[i]
		if c == '#' {
			for j := i; j < len(buf); j++ {
				buf[j] = ' '
			}
			break
		}
		if c == '\'' || c == '"' {
			if i+2 < len(buf) && buf[i+1] == c && buf[i+2] == c {
				m.triple = c
				for j := i; j < i+3; j++ {
					buf[j] = ' '
				}
				i += 3
				continue
			}
			j := i + 1
			for j < len(buf) {
				if buf[j] == '\\' {
					j += 2
					continue
				}
				if buf[j] == c {
					break
				}
				j++
			}
			if j >= len(buf) {
				j = len(buf) - 1
			}
			for k := i; k <= j && k < len(buf); k++ {
				buf[k] = ' '
			}
			i = j + 1
			continue
		}
		i++
	}
	return string(buf)
}

func pyIndent(line string) int {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 8 - w%8
		case '\f':
			w = 0
		default:
			return w
		}
	}
	return w
}

func parsePyClassName(s string) (string, []string) {
	s = strings.TrimPrefix(s, "class ")
	s = strings.TrimSpace(s)
	if b := strings.IndexByte(s, '['); b >= 0 {
		depth := 0
		for i := b; i < len(s); i++ {
			if s[i] == '[' {
				depth++
			} else if s[i] == ']' {
				depth--
				if depth == 0 {
					s = strings.TrimSpace(s[:b] + s[i+1:])
					break
				}
			}
		}
	}
	end := 0
	for end < len(s) && (isIdentByte(s[end]) || s[end] >= 0x80) {
		end++
	}
	name := s[:end]
	if name == "" {
		return "", nil
	}
	rest := strings.TrimSpace(s[end:])
	var bases []string
	if strings.HasPrefix(rest, "(") {
		if close := matchParenString(rest, 0); close > 0 {
			bases = splitTopLevel(rest[1:close], ',')
		}
	}
	clean := make([]string, 0, len(bases))
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" || strings.ContainsAny(base, "=*()[]") {
			continue
		}
		clean = append(clean, base)
	}
	return name, clean
}

func parsePyFuncName(s string) string {
	s = strings.TrimPrefix(s, "async ")
	s = strings.TrimPrefix(s, "def ")
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (isIdentByte(s[end]) || s[end] >= 0x80) {
		end++
	}
	return s[:end]
}

func isPyIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 {
			continue
		}
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func matchParenString(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// parsePyImport handles `import a.b as c, d` and `from .x import (a, b)`.
func parsePyImport(b *Builder, trimmed string, line int) {
	switch {
	case strings.HasPrefix(trimmed, "import "):
		rest := strings.TrimPrefix(trimmed, "import ")
		for _, part := range splitTopLevel(rest, ',') {
			part = strings.TrimSpace(part)
			if k := strings.Index(part, " as "); k >= 0 {
				part = strings.TrimSpace(part[:k])
			}
			part = strings.TrimLeft(part, ".")
			if part == "" {
				continue
			}
			b.Edge(b.FileID, MakeID(part), "imports", line)
		}
	case strings.HasPrefix(trimmed, "from "):
		rest := strings.TrimPrefix(trimmed, "from ")
		idx := strings.Index(rest, " import ")
		if idx < 0 {
			return
		}
		mod := strings.TrimLeft(strings.TrimSpace(rest[:idx]), ".")
		if mod == "" {
			return
		}
		b.Edge(b.FileID, MakeID(mod), "imports_from", line)
	}
}

func scanPyCalls(b *Builder, caller, masked string, line int) {
	for i := 0; i < len(masked); i++ {
		if masked[i] != '(' {
			continue
		}
		j := i - 1
		for j >= 0 && (masked[j] == ' ' || masked[j] == '\t') {
			j--
		}
		end := j + 1
		for j >= 0 && (isIdentByte(masked[j]) || masked[j] >= 0x80) {
			j--
		}
		name := masked[j+1 : end]
		if name == "" || pyCallSkip[name] {
			continue
		}
		if id := b.Lookup(name); id != "" {
			b.Call(caller, id, name, line)
		}
	}
}

var pyCallSkip = map[string]bool{
	"if": true, "for": true, "while": true, "return": true, "def": true,
	"class": true, "with": true, "elif": true, "assert": true, "yield": true,
	"lambda": true, "except": true, "raise": true, "del": true, "import": true,
	"from": true, "not": true, "and": true, "or": true, "in": true, "await": true,
}
