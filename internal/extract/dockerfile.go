package extract

import (
	"os"
	"regexp"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

var (
	dockerFromRe = regexp.MustCompile(`(?i)^\s*FROM\s+(\S+)(?:\s+AS\s+(\S+))?`)
	dockerCopyRe = regexp.MustCompile(`(?i)--from=(\S+)`)
)

// extractDockerfile models build stages: every FROM is a node and COPY
// --from=<stage> links stages together.
func extractDockerfile(path, stem string) model.Extraction {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Extraction{}
	}
	b := NewBuilder(path, stem, model.FileTypeCode)
	stage := func(name string) (string, string) {
		return MakeID(stem, "stage", name), "stage " + name
	}
	cur := ""
	for i, raw := range splitLines(string(data)) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := dockerFromRe.FindStringSubmatch(line); m != nil {
			name := m[2]
			if name == "" {
				name = m[1]
			}
			id, label := stage(name)
			b.AddNode(id, label, i+1)
			b.Edge(b.FileID, id, "contains", i+1)
			cur = id
			continue
		}
		if cur == "" {
			continue
		}
		for _, m := range dockerCopyRe.FindAllStringSubmatch(line, -1) {
			id, label := stage(m[1])
			b.AddStub(id, label)
			b.EdgeConf(cur, id, "copies_from", i+1, model.ConfInferred, 0.8)
		}
	}
	return b.Result()
}
