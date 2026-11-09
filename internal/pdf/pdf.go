// Package pdf implements a dependency-free, heuristic PDF text extractor.
//
// It is intentionally not a conforming PDF parser: it scans indirect objects
// for stream data, inflates FlateDecode streams with the standard library and
// replays text-showing operators from content streams. This is enough for
// corpus analysis (word counts, section headings, document nodes) and is
// dramatically faster than shipping a full PDF engine.
package pdf

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// ErrNoText is returned when no text content could be recovered.
var ErrNoText = errors.New("pdf: no extractable text")

// ExtractText returns the recovered text of the PDF at path.
func ExtractText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return ExtractTextBytes(data)
}

// ExtractTextBytes returns recovered text from in-memory PDF bytes.
func ExtractTextBytes(data []byte) (string, error) {
	var out strings.Builder
	// Scan for stream ... endstream pairs. Binary streams that fail to
	// inflate or that contain no text operators are silently skipped.
	pos := 0
	for {
		i := bytes.Index(data[pos:], []byte("stream"))
		if i < 0 {
			break
		}
		i += pos
		// Guard against "endstream".
		if i >= 5 && string(data[i-3:i]) == "end" {
			pos = i + 6
			continue
		}
		start := i + 6
		if start < len(data) && data[start] == '\r' {
			start++
		}
		if start < len(data) && data[start] == '\n' {
			start++
		}
		endRel := bytes.Index(data[start:], []byte("endstream"))
		if endRel < 0 {
			break
		}
		end := start + endRel
		raw := data[start:end]
		pos = end + 9

		dictStart := bytes.LastIndex(data[:i], []byte("<<"))
		var dict []byte
		if dictStart >= 0 {
			dict = data[dictStart:i]
		}
		payload, ok := decodeStream(raw, dict)
		if !ok {
			continue
		}
		if !looksLikeContent(payload) {
			continue
		}
		text := contentText(payload)
		if text != "" {
			out.WriteString(text)
			out.WriteByte('\n')
			if out.Len() > 8<<20 {
				break
			}
		}
	}
	if out.Len() == 0 {
		return "", ErrNoText
	}
	return out.String(), nil
}

func decodeStream(raw, dict []byte) ([]byte, bool) {
	isFlate := bytes.Contains(dict, []byte("FlateDecode")) || bytes.Contains(dict, []byte("Fl "))
	if !isFlate {
		return raw, true
	}
	// Most PDFs use zlib; a few use raw deflate.
	if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, 32<<20))
		if err == nil {
			return out, true
		}
	}
	fr := flate.NewReader(bytes.NewReader(raw))
	defer fr.Close()
	out, err := io.ReadAll(io.LimitReader(fr, 32<<20))
	if err != nil {
		return nil, false
	}
	return out, true
}

func looksLikeContent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if !bytes.Contains(b, []byte("BT")) && !bytes.Contains(b, []byte("Tj")) &&
		!bytes.Contains(b, []byte("TJ")) {
		return false
	}
	// Reject streams dominated by control bytes (fonts, images).
	printable, total := 0, 0
	for _, c := range b {
		if total >= 4096 {
			break
		}
		total++
		if c == 9 || c == 10 || c == 13 || (c >= 32 && c < 127) {
			printable++
		}
	}
	return total == 0 || printable*10 >= total*7
}

// contentText replays a content stream and returns visible text.
func contentText(b []byte) string {
	var out strings.Builder
	var pending []string
	var arrayMode bool
	lastWasGap := false

	flush := func() {
		for _, s := range pending {
			out.WriteString(s)
		}
		pending = pending[:0]
	}

	i := 0
	for i < len(b) {
		c := b[i]
		switch {
		case c == '(':
			s, next := readLiteral(b, i)
			pending = append(pending, applyGap(s, lastWasGap))
			lastWasGap = false
			i = next
		case c == '<' && i+1 < len(b) && b[i+1] != '<':
			s, next := readHex(b, i)
			pending = append(pending, applyGap(s, lastWasGap))
			lastWasGap = false
			i = next
		case c == '[':
			arrayMode = true
			i++
		case c == ']':
			arrayMode = false
			i++
		case c == '-' || (c >= '0' && c <= '9') || c == '.':
			num, next := readNumber(b, i)
			// A large negative kerning value inside TJ separates words.
			if arrayMode && num <= -120 {
				lastWasGap = true
			}
			i = next
		case isAlpha(c):
			op, next := readOperator(b, i)
			i = next
			switch op {
			case "Tj", "TJ":
				flush()
			case "'", "\"":
				out.WriteByte('\n')
				flush()
			case "Td", "TD", "T*", "TL", "ET":
				out.WriteByte('\n')
				flush()
			}
		default:
			i++
		}
	}
	flush()
	// Collapse runs of blank lines.
	s := out.String()
	var clean strings.Builder
	blank := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		clean.WriteString(line)
		clean.WriteByte('\n')
	}
	return strings.TrimSpace(clean.String())
}

func applyGap(s string, gap bool) string {
	if gap && s != "" {
		return " " + s
	}
	return s
}

func readLiteral(b []byte, i int) (string, int) {
	i++ // skip '('
	var out []byte
	depth := 1
	for i < len(b) {
		c := b[i]
		if c == '\\' {
			i++
			if i >= len(b) {
				break
			}
			switch b[i] {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '(', ')', '\\':
				out = append(out, b[i])
			case '\r':
				if i+1 < len(b) && b[i+1] == '\n' {
					i++
				}
			case '\n':
				// line continuation
			default:
				if b[i] >= '0' && b[i] <= '7' {
					v := 0
					for k := 0; k < 3 && i < len(b) && b[i] >= '0' && b[i] <= '7'; k++ {
						v = v*8 + int(b[i]-'0')
						i++
					}
					out = append(out, byte(v))
					i--
				} else {
					out = append(out, b[i])
				}
			}
			i++
			continue
		}
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
			if depth == 0 {
				i++
				break
			}
		}
		out = append(out, c)
		i++
	}
	return decodePDFString(out), i
}

func readHex(b []byte, i int) (string, int) {
	i++ // '<'
	var out []byte
	var hi = -1
	for i < len(b) && b[i] != '>' {
		c := b[i]
		v := -1
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		}
		if v >= 0 {
			if hi < 0 {
				hi = v
			} else {
				out = append(out, byte(hi<<4|v))
				hi = -1
			}
		}
		i++
	}
	if hi >= 0 {
		out = append(out, byte(hi<<4))
	}
	if i < len(b) {
		i++
	}
	return decodePDFString(out), i
}

func decodePDFString(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		u := make([]uint16, 0, (len(raw)-2)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			u = append(u, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		return string(utf16.Decode(u))
	}
	// PDFDocEncoding is close enough to Latin-1 for extraction purposes.
	var sb strings.Builder
	for _, c := range raw {
		if c >= 32 || c == '\n' || c == '\t' {
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

func readNumber(b []byte, i int) (float64, int) {
	start := i
	if b[i] == '-' {
		i++
	}
	for i < len(b) && ((b[i] >= '0' && b[i] <= '9') || b[i] == '.') {
		i++
	}
	var v float64
	neg := false
	j := start
	if j < len(b) && b[j] == '-' {
		neg = true
		j++
	}
	for ; j < i; j++ {
		if b[j] == '.' {
			scale := 0.1
			for k := j + 1; k < i; k++ {
				v += float64(b[k]-'0') * scale
				scale *= 0.1
			}
			break
		}
		v = v*10 + float64(b[j]-'0')
	}
	if neg {
		v = -v
	}
	return v, i
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '\'' || c == '"'
}

func readOperator(b []byte, i int) (string, int) {
	start := i
	for i < len(b) && (isAlpha(b[i]) || b[i] == '*') {
		i++
	}
	return string(b[start:i]), i
}
