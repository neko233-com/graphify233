package enrich

import (
	"fmt"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/extract"
	"github.com/neko233-com/graphify233/internal/model"
)

// conceptDocMax keeps heading extraction bounded per file.
const conceptDocMax = 200

// conceptMentionsMax bounds links from one concept to code symbols.
const conceptMentionsMax = 20

// Concepts turns Markdown headings into document concept nodes and links each
// concept to the code symbols mentioned in its section. The doc file node
// "contains" every concept (EXTRACTED); concept-to-symbol edges are INFERRED
// "mentions" edges tagged with the line where the symbol occurs.
func Concepts(g *model.Graph, docs []Doc, lim Limits) (concepts, mentions int) {
	lim = lim.withDefaults()
	if len(g.Nodes) == 0 || len(docs) == 0 {
		return 0, 0
	}
	byLabel := symbolIndex(g)
	fileNode := fileNodeIndex(g)
	if len(byLabel) == 0 && len(fileNode.exact) == 0 {
		return 0, 0
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Rel < docs[j].Rel })

	for _, d := range docs {
		if !isMarkdown(d.Rel) {
			continue
		}
		src, ok := fileNode.find(d.Rel)
		if !ok {
			continue
		}
		text, err := read(d, lim.MaxBytes)
		if err != nil || text == "" {
			continue
		}
		lines := splitLines(text)
		headings := parseHeadings(lines, conceptDocMax)
		if len(headings) == 0 {
			continue
		}
		stem := strings.TrimSuffix(baseName(d.Rel), extOf(d.Rel))
		for hi, h := range headings {
			end := len(lines)
			if hi+1 < len(headings) {
				end = headings[hi+1].line
			}
			id := extract.MakeID(stem, h.title)
			if id == "" {
				continue
			}
			idx := g.Index(id)
			if idx < 0 {
				idx = g.AddNode(model.Node{
					ID:             id,
					Label:          h.title,
					FileType:       model.FileTypeDocument,
					SourceFile:     d.Rel,
					SourceLocation: fmt.Sprintf("L%d", h.line+1),
					Community:      -1,
				})
				concepts++
			}
			conceptID := g.Nodes[idx].ID
			if !g.HasEdge(g.Nodes[src].ID, conceptID) {
				g.AddEdge(model.Edge{
					Source:          g.Nodes[src].ID,
					Target:          conceptID,
					Relation:        "contains",
					Confidence:      model.ConfExtracted,
					SourceFile:      d.Rel,
					SourceLocation:  fmt.Sprintf("L%d", h.line+1),
					Weight:          1,
					ConfidenceScore: 1,
				})
			}
			perConcept := 0
			for li := h.line + 1; li < end && perConcept < conceptMentionsMax; li++ {
				for _, tok := range identifiers(lines[li]) {
					sym, ok := byLabel[strings.ToLower(tok)]
					if !ok {
						continue
					}
					target := g.Nodes[sym]
					if target.ID == conceptID || g.HasEdge(conceptID, target.ID) {
						continue
					}
					if g.AddEdge(model.Edge{
						Source:          conceptID,
						Target:          target.ID,
						Relation:        "mentions",
						Confidence:      model.ConfInferred,
						SourceFile:      d.Rel,
						SourceLocation:  fmt.Sprintf("L%d", li+1),
						Weight:          0.6,
						ConfidenceScore: 0.6,
					}) {
						mentions++
						perConcept++
						if perConcept >= conceptMentionsMax {
							break
						}
					}
				}
			}
		}
	}
	return concepts, mentions
}

type heading struct {
	line  int // 0-based
	title string
}

func parseHeadings(lines []string, max int) []heading {
	var out []heading
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level > 6 || level == len(trimmed) {
			continue
		}
		title := strings.TrimSpace(trimmed[level:])
		title = strings.TrimSuffix(title, "#")
		title = strings.TrimSpace(title)
		title = cleanHeading(title)
		if title == "" {
			continue
		}
		out = append(out, heading{line: i, title: title})
		if len(out) >= max {
			break
		}
	}
	return out
}

// cleanHeading strips Markdown emphasis and link syntax so the concept label is
// readable and stable.
func cleanHeading(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "[", "")
	s = strings.ReplaceAll(s, "]", "")
	if i := strings.Index(s, "]("); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = strings.TrimSpace(s[:100])
	}
	return s
}

func isMarkdown(p string) bool {
	p = strings.ToLower(p)
	return strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".markdown")
}

func extOf(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "."); i >= 0 && i > strings.LastIndex(p, "/") {
		return p[i:]
	}
	return ""
}
