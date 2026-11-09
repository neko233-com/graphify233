package extract

import (
	"os"
	"regexp"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

var (
	tfTwoNameRe = regexp.MustCompile(`^\s*(resource|data)\s+"([^"]+)"\s+"([^"]+)"\s*\{`)
	tfOneNameRe = regexp.MustCompile(`^\s*(module|variable|output|provider)\s+"([^"]+)"\s*\{`)
	tfVarRefRe  = regexp.MustCompile(`\b(var|module|local|each|count|path|terraform)\.([A-Za-z_][\w-]*)`)
	tfDataRefRe = regexp.MustCompile(`\bdata\.([A-Za-z_][\w-]*)\.([A-Za-z_][\w-]*)`)
	tfResRefRe  = regexp.MustCompile(`\b([A-Za-z][\w-]*)\.([A-Za-z_][\w-]*)`)
)

var tfReserved = map[string]bool{
	"var": true, "module": true, "local": true, "each": true, "count": true,
	"path": true, "terraform": true, "data": true,
}

// extractTerraform maps HCL blocks (resource, data, module, variable, output,
// provider) to nodes and turns variable/module/resource references into edges.
func extractTerraform(path, stem string) model.Extraction {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Extraction{}
	}
	b := NewBuilder(path, stem, model.FileTypeCode)
	seen := make(map[string]bool, 32)
	ref := func(src, tgt, label string, line int) {
		key := src + "\x00" + tgt
		if src == "" || tgt == "" || seen[key] {
			return
		}
		seen[key] = true
		b.AddStub(tgt, label)
		b.EdgeConf(src, tgt, "references", line, model.ConfInferred, 0.8)
	}

	depth := 0
	cur := ""
	for i, raw := range splitLines(string(data)) {
		line := stripTFComment(raw)
		lineNo := i + 1
		opened := false
		if cur == "" {
			if m := tfTwoNameRe.FindStringSubmatch(line); m != nil {
				kind, typ, name := strings.ToLower(m[1]), m[2], m[3]
				label := typ + "." + name
				if kind == "data" {
					label = "data." + typ + "." + name
				}
				id := MakeID(stem, kind, typ, name)
				b.AddNode(id, label, lineNo)
				b.Edge(b.FileID, id, "contains", lineNo)
				cur = id
				opened = true
			} else if m := tfOneNameRe.FindStringSubmatch(line); m != nil {
				kind, name := strings.ToLower(m[1]), m[2]
				label := kind + "." + name
				id := MakeID(stem, kind, name)
				b.AddNode(id, label, lineNo)
				b.Edge(b.FileID, id, "contains", lineNo)
				cur = id
				opened = true
			}
		}
		if cur == "" {
			continue
		}
		for _, m := range tfDataRefRe.FindAllStringSubmatch(line, -1) {
			ref(cur, MakeID(stem, "data", m[1], m[2]), "data."+m[1]+"."+m[2], lineNo)
		}
		for _, m := range tfVarRefRe.FindAllStringSubmatch(line, -1) {
			kind, name := strings.ToLower(m[1]), m[2]
			ref(cur, MakeID(stem, kind, name), kind+"."+name, lineNo)
		}
		for _, m := range tfResRefRe.FindAllStringSubmatch(line, -1) {
			typ, name := m[1], m[2]
			if tfReserved[strings.ToLower(typ)] {
				continue
			}
			ref(cur, MakeID(stem, typ, name), typ+"."+name, lineNo)
		}
		if opened {
			depth = braceDelta(line)
		} else {
			depth += braceDelta(line)
		}
		if depth <= 0 {
			cur = ""
		}
	}
	return b.Result()
}

func braceDelta(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '{':
			n++
		case '}':
			n--
		}
	}
	return n
}

func stripTFComment(line string) string {
	inString := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inString = !inString
		case '#':
			if !inString {
				return line[:i]
			}
		case '/':
			if !inString && i+1 < len(line) && line[i+1] == '/' {
				return line[:i]
			}
		}
	}
	return line
}
