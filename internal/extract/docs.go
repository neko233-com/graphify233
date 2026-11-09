package extract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/pdf"
)

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	mdLink    = regexp.MustCompile(`\[([^\]]{1,120})\]\(([^)\s]{1,300})\)`)
	wikiLink  = regexp.MustCompile(`\[\[([^\]|]{1,120})(?:\|[^\]]*)?\]\]`)
)

// extractDoc turns prose into a small concept graph: every heading becomes a
// concept owned by the file, and links to other files/documents become
// "references" edges. Deterministic and dependency-free, in the spirit of the
// code extractors.
func extractDoc(path, stem string, kind model.FileType) model.Extraction {
	b := NewBuilder(path, stem, kind)
	data, err := os.ReadFile(path)
	if err != nil {
		return b.Result()
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	dir := filepath.Dir(path)

	seenHeading := make(map[string]bool)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if m := mdHeading.FindStringSubmatch(t); m != nil {
			label := strings.TrimSpace(strings.Trim(m[2], "#"))
			if label == "" {
				continue
			}
			key := strings.ToLower(label)
			if seenHeading[key] {
				continue
			}
			seenHeading[key] = true
			nid := MakeID(stem, label)
			b.AddNode(nid, label, i+1)
			b.Edge(b.FileID, nid, "contains", i+1)
			continue
		}
		// [[wiki links]] become relations between documents/concepts.
		for _, m := range wikiLink.FindAllStringSubmatch(t, -1) {
			target := strings.TrimSpace(m[1])
			if target == "" || strings.EqualFold(target, stem) {
				continue
			}
			b.AddStub(MakeID(target), target)
			b.Edge(b.FileID, MakeID(target), "references", i+1)
		}
		// Relative markdown links to local files become file-to-file edges.
		for _, m := range mdLink.FindAllStringSubmatch(t, -1) {
			target := strings.TrimSpace(m[2])
			if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, target))
			tstem := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
			if strings.EqualFold(tstem, stem) {
				continue
			}
			b.AddStub(MakeID(tstem), tstem)
			b.Edge(b.FileID, MakeID(tstem), "references", i+1)
		}
	}
	return b.Result()
}

// extractPaper reads a PDF deterministically and promotes its title and
// abstract to concept nodes.
func extractPaper(path, stem string) model.Extraction {
	b := NewBuilder(path, stem, model.FileTypePaper)
	text, err := pdf.ExtractText(path)
	if err != nil || strings.TrimSpace(text) == "" {
		return b.Result()
	}
	paras := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var title string
	var body strings.Builder
	line := 1
	for _, p := range paras {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		low := strings.ToLower(t)
		if title == "" && len(t) > 3 && len(t) < 200 && !strings.Contains(low, "arxiv") && !strings.HasPrefix(low, "abstract") {
			title = t
			continue
		}
		if strings.HasPrefix(low, "abstract") {
			body.WriteString(t)
			body.WriteByte(' ')
			continue
		}
		if body.Len() < 1200 && len(t) > 40 {
			body.WriteString(t)
			body.WriteByte(' ')
		}
	}
	if title != "" {
		nid := MakeID(stem, title)
		b.AddNode(nid, truncateRunes(title, 90), line)
		b.Edge(b.FileID, nid, "contains", line)
	}
	if body.Len() > 0 {
		label := "abstract: " + truncateRunes(firstSentence(body.String()), 90)
		nid := MakeID(stem, "abstract")
		b.AddNode(nid, label, line)
		b.Edge(b.FileID, nid, "contains", line)
	}
	return b.Result()
}

// extractImage keeps the file node; pixels are out of scope for a deterministic
// pass, but the node still participates in clustering and reports.
func extractImage(path, stem string) model.Extraction {
	return NewBuilder(path, stem, model.FileTypeImage).Result()
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func firstSentence(s string) string {
	for i, r := range s {
		if r == '.' && i > 20 {
			return s[:i]
		}
	}
	return s
}
