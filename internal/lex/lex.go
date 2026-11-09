// Package lex provides a small, fast, dependency-free tokenizer shared by the
// structural extractors. It is deliberately lossy: comments are dropped and
// only the token classes needed for declaration / call discovery are kept.
package lex

import "strings"

// Kind classifies a token.
type Kind uint8

const (
	Ident Kind = iota
	String
	Number
	Punct
)

// Token is one lexical unit. Text is a zero-copy slice of the source string.
type Token struct {
	Kind  Kind
	Text  string
	Line  int32
	Start int32
	End   int32
}

// Quote describes a string literal form.
type Quote struct {
	Open      byte
	Close     byte
	Escape    bool
	Multiline bool
	Raw       bool // backtick / verbatim: no escapes
	Triple    bool // python-style triple quote (Open repeated 3x)
}

// Config configures the tokenizer for a language family.
type Config struct {
	LineComments  []string
	BlockComments [][2]string
	NestedBlock   bool
	Quotes        []Quote
	RegexLiterals bool // JS: recognise and skip /regex/flags
}

// CLike is the configuration shared by Go, JS/TS, Rust, Java, C, C++, C#,
// Kotlin, Scala, PHP.
var CLike = Config{
	LineComments:  []string{"//"},
	BlockComments: [][2]string{{"/*", "*/"}},
	Quotes: []Quote{
		{Open: '"', Close: '"', Escape: true, Multiline: false},
		{Open: '\'', Close: '\'', Escape: true, Multiline: false},
		{Open: '`', Close: '`', Raw: true, Multiline: true},
	},
}

// JS adds regex literals and template strings (backticks already covered).
func JS() Config {
	c := CLike
	c.RegexLiterals = true
	return c
}

// Hash is for Ruby / shell-like languages.
func Hash() Config {
	return Config{
		LineComments: []string{"#"},
		Quotes: []Quote{
			{Open: '"', Close: '"', Escape: true, Multiline: true},
			{Open: '\'', Close: '\'', Escape: true, Multiline: true},
		},
	}
}

// IsIdentStart reports whether b can start an identifier.
func IsIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// IsIdentPart reports whether b can continue an identifier.
func IsIdentPart(b byte) bool {
	return IsIdentStart(b) || (b >= '0' && b <= '9')
}

// Tokenize scans src into tokens.
func Tokenize(src string, cfg Config) []Token {
	toks := make([]Token, 0, len(src)/4+8)
	line := int32(1)
	n := len(src)
	i := 0

	for i < n {
		c := src[i]

		// Whitespace (fast path for the common newline/space).
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' {
			i++
			continue
		}

		// Line comments.
		if len(cfg.LineComments) > 0 {
			matched := false
			for _, p := range cfg.LineComments {
				if strings.HasPrefix(src[i:], p) {
					j := strings.IndexByte(src[i:], '\n')
					if j < 0 {
						i = n
					} else {
						i += j
					}
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}

		// Block comments.
		if len(cfg.BlockComments) > 0 {
			matched := false
			for _, p := range cfg.BlockComments {
				if strings.HasPrefix(src[i:], p[0]) {
					j := strings.Index(src[i+len(p[0]):], p[1])
					if j < 0 {
						line += countNL(src[i:])
						i = n
					} else {
						end := i + len(p[0]) + j + len(p[1])
						line += countNL(src[i:end])
						i = end
					}
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}

		// Strings.
		if q, ok := matchQuote(src, i, cfg.Quotes); ok {
			start := i
			startLine := line
			tok, next, nl := scanString(src, i, q)
			line += nl
			toks = append(toks, Token{Kind: String, Text: tok, Line: startLine, Start: int32(start), End: int32(next)})
			i = next
			continue
		}

		// JS regex literal.
		if cfg.RegexLiterals && c == '/' && regexAllowed(src, i) {
			if next, nl, ok := scanRegex(src, i); ok {
				line += nl
				i = next
				continue
			}
		}

		// Numbers.
		if (c >= '0' && c <= '9') || (c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9') {
			start := i
			i++
			for i < n && (IsIdentPart(src[i]) || src[i] == '.' || src[i] == '\'') {
				// Avoid swallowing "1.foo" member calls; only consume hex
				// letters directly after 0x.
				if (src[i] >= 'a' && src[i] <= 'z') || (src[i] >= 'A' && src[i] <= 'Z') {
					if !(i >= start+2 && src[start] == '0' && (src[start+1] == 'x' || src[start+1] == 'X' || src[start+1] == 'b' || src[start+1] == 'o')) {
						break
					}
				}
				i++
			}
			toks = append(toks, Token{Kind: Number, Text: src[start:i], Line: line, Start: int32(start), End: int32(i)})
			continue
		}

		// Identifiers.
		if IsIdentStart(c) {
			start := i
			i++
			for i < n && IsIdentPart(src[i]) {
				i++
			}
			toks = append(toks, Token{Kind: Ident, Text: src[start:i], Line: line, Start: int32(start), End: int32(i)})
			continue
		}

		// Punctuation / operators.
		start := i
		i++
		if i < n {
			two := src[start : i+1]
			switch two {
			case "=>", "->", "::", "...", "==", "!=", "<=", ">=", "&&", "||",
				"??", "?.", "++", "--", "+=", "-=", "*=", "/=", ":=", "<<", ">>", "**":
				i++
			}
		}
		toks = append(toks, Token{Kind: Punct, Text: src[start:i], Line: line, Start: int32(start), End: int32(i)})
	}
	return toks
}

func countNL(s string) int32 {
	var c int32
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			c++
		}
	}
	return c
}

func matchQuote(src string, i int, quotes []Quote) (Quote, bool) {
	for _, q := range quotes {
		if src[i] == q.Open {
			return q, true
		}
	}
	return Quote{}, false
}

func scanString(src string, i int, q Quote) (string, int, int32) {
	n := len(src)
	i++
	if q.Triple {
		i += 2
	}
	start := i
	var nl int32
	depth := 0
	escaped := false
	for i < n {
		c := src[i]
		if c == '\n' {
			nl++
			if !q.Multiline && !q.Triple {
				break
			}
		}
		if q.Escape && !q.Raw && escaped {
			escaped = false
			if c == '\n' {
				nl++
			}
			i++
			continue
		}
		if q.Escape && !q.Raw && c == '\\' {
			escaped = true
			i++
			continue
		}
		if q.Triple && strings.HasPrefix(src[i:], strings.Repeat(string(q.Close), 3)) {
			s := decodeEscapes(src[start:i], q.Raw)
			return s, i + 3, nl
		}
		if !q.Triple && c == q.Close {
			s := decodeEscapes(src[start:i], q.Raw)
			return s, i + 1, nl
		}
		if q.Open != q.Close && c == q.Open {
			depth++
		}
		_ = depth
		i++
	}
	return decodeEscapes(src[start:n], q.Raw), n, nl
}

func decodeEscapes(s string, raw bool) string {
	if raw || !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case '\'':
			b.WriteByte('\'')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func regexAllowed(src string, i int) bool {
	j := i - 1
	for j >= 0 {
		c := src[j]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			j--
			continue
		}
		switch c {
		case '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', ';', '>', '+', '-', '*', '%', '^', '~':
			return true
		case 'n': // return
			return strings.HasSuffix(src[:j+1], "return")
		}
		return false
	}
	return true
}

func scanRegex(src string, i int) (int, int32, bool) {
	n := len(src)
	i++
	var nl int32
	inClass := false
	for i < n {
		c := src[i]
		if c == '\n' {
			return 0, 0, false // not a regex literal; let division tokenize
		}
		if c == '\\' {
			i += 2
			continue
		}
		if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '/' && !inClass {
			i++
			for i < n && IsIdentPart(src[i]) {
				i++
			}
			return i, nl, true
		}
		i++
	}
	return 0, 0, false
}
