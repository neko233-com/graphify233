package extract

import (
	"os"
	"regexp"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

var (
	sqlCreateRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?(?:UNIQUE\s+)?(TABLE|VIEW|INDEX|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|TYPE)\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)`)
	sqlRefRe    = regexp.MustCompile(`(?i)\bREFERENCES\s+([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)`)
	sqlFromRe   = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)`)
	sqlOnRe     = regexp.MustCompile(`(?i)\bON\s+([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)`)
)

// extractSQL builds nodes for tables, views, indexes and functions plus
// foreign-key and view-body references. SQL DDL is line-oriented enough for
// deterministic extraction; comments are skipped.
func extractSQL(path, stem string) model.Extraction {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Extraction{}
	}
	b := NewBuilder(path, stem, model.FileTypeCode)
	seen := make(map[string]bool, 32)
	addEdge := func(src, tgt, rel string, line int) {
		key := src + "\x00" + tgt + "\x00" + rel
		if src == "" || tgt == "" || seen[key] {
			return
		}
		seen[key] = true
		b.Edge(src, tgt, rel, line)
	}
	bodyID := "" // relation whose body we are inside
	isView := false
	depth := 0
	inBlockComment := false
	for i, raw := range splitLines(string(data)) {
		line, inBlock := stripSQLComments(raw, inBlockComment)
		inBlockComment = inBlock
		lineNo := i + 1
		if m := sqlCreateRe.FindStringSubmatch(line); m != nil {
			kind := strings.ToUpper(strings.Join(strings.Fields(m[1]), " "))
			name := m[2]
			id := MakeID(stem, name)
			b.AddNode(id, name, lineNo)
			addEdge(b.FileID, id, "contains", lineNo)
			switch kind {
			case "TABLE", "VIEW", "MATERIALIZED VIEW":
				bodyID, isView = id, kind != "TABLE"
				depth = strings.Count(line, "(") - strings.Count(line, ")")
			case "INDEX":
				if m := sqlOnRe.FindStringSubmatch(line); m != nil {
					tid := MakeID(stem, m[1])
					b.AddStub(tid, m[1])
					addEdge(id, tid, "references", lineNo)
				}
			}
		}
		if bodyID != "" {
			for _, m := range sqlRefRe.FindAllStringSubmatch(line, -1) {
				tid := MakeID(stem, m[1])
				b.AddStub(tid, m[1])
				addEdge(bodyID, tid, "references", lineNo)
			}
			if isView {
				for _, m := range sqlFromRe.FindAllStringSubmatch(line, -1) {
					tid := MakeID(stem, m[1])
					b.AddStub(tid, m[1])
					addEdge(bodyID, tid, "references", lineNo)
				}
			}
		}
		if bodyID != "" {
			depth += strings.Count(line, "(") - strings.Count(line, ")")
			if depth <= 0 {
				bodyID, isView = "", false
			}
		}
	}
	return b.Result()
}

func stripSQLComments(line string, inBlock bool) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		if inBlock {
			if line[i] == '*' && i+1 < len(line) && line[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if line[i] == '-' && i+1 < len(line) && line[i+1] == '-' {
			break
		}
		if line[i] == '/' && i+1 < len(line) && line[i+1] == '*' {
			inBlock = true
			i++
			continue
		}
		out.WriteByte(line[i])
	}
	return out.String(), inBlock
}
