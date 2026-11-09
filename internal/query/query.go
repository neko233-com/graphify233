// Package query answers graph questions locally: keyword scoring, bounded
// BFS subgraphs, node explanations and shortest paths.
package query

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/neko233-com/graphify233/internal/model"
)

// ErrNotFound is returned when a name cannot be resolved.
var ErrNotFound = fmt.Errorf("node not found")

// Engine queries a built graph.
type Engine struct {
	G *model.Graph
}

// New wraps a graph.
func New(g *model.Graph) *Engine { return &Engine{G: g} }

var stopTerms = map[string]bool{
	"the": true, "and": true, "how": true, "does": true, "what": true, "are": true,
	"for": true, "that": true, "this": true, "with": true, "from": true, "into": true,
	"work": true, "works": true, "main": true, "used": true, "use": true, "when": true,
	"where": true, "why": true, "who": true, "which": true, "there": true, "their": true,
}

// Terms splits a question into lower-cased search terms.
func Terms(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		if len(f) <= 2 || stopTerms[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// Score returns nodes ranked by how many query terms their label/ID contains.
func (e *Engine) Score(q string) []int32 {
	terms := Terms(q)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		idx   int32
		score int
	}
	hits := make([]scored, 0, 8)
	for i := range e.G.Nodes {
		label := strings.ToLower(e.G.Nodes[i].Label)
		id := e.G.Nodes[i].ID
		score := 0
		for _, t := range terms {
			if strings.Contains(label, t) {
				score += 2
			} else if strings.Contains(id, t) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{int32(i), score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		di, dj := e.G.Degree(hits[i].idx), e.G.Degree(hits[j].idx)
		if di != dj {
			return di > dj
		}
		return e.G.Nodes[hits[i].idx].Label < e.G.Nodes[hits[j].idx].Label
	})
	out := make([]int32, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// BFS collects nodes and edges within depth hops of the start nodes.
func (e *Engine) BFS(start []int32, depth int) ([]int32, []int32) {
	if depth <= 0 {
		depth = 3
	}
	seen := make([]bool, len(e.G.Nodes))
	var nodes []int32
	queue := append([]int32(nil), start...)
	for _, s := range start {
		seen[s] = true
		nodes = append(nodes, s)
	}
	frontier := queue
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []int32
		for _, u := range frontier {
			for _, v := range e.G.Adj()[u] {
				if !seen[v] {
					seen[v] = true
					nodes = append(nodes, v)
					next = append(next, v)
				}
			}
		}
		frontier = next
	}
	var edges []int32
	for i := range e.G.Edges {
		if e.G.Index(e.G.Edges[i].Source) < 0 {
			continue
		}
		s := int32(e.G.Index(e.G.Edges[i].Source))
		t := int32(e.G.Index(e.G.Edges[i].Target))
		if s < 0 || t < 0 {
			continue
		}
		if seen[s] && seen[t] {
			edges = append(edges, int32(i))
		}
	}
	return nodes, edges
}

// SubgraphText renders a token-budgeted textual subgraph for LLM consumption.
func (e *Engine) SubgraphText(start []int32, depth, tokenBudget int) string {
	if tokenBudget <= 0 {
		tokenBudget = 2000
	}
	nodes, edges := e.BFS(start, depth)
	var b strings.Builder
	for _, n := range nodes {
		node := e.G.Nodes[n]
		src := node.SourceFile
		if src == "" {
			src = "external"
		}
		fmt.Fprintf(&b, "NODE %s src=%s loc=%s\n", node.Label, src, node.SourceLocation)
		if b.Len()/4 > tokenBudget {
			return b.String()
		}
	}
	for _, ei := range edges {
		edge := e.G.Edges[ei]
		fmt.Fprintf(&b, "EDGE %s --%s--> %s\n",
			e.G.Nodes[e.G.Index(edge.Source)].Label, edge.Relation, e.G.Nodes[e.G.Index(edge.Target)].Label)
		if b.Len()/4 > tokenBudget {
			return b.String()
		}
	}
	return b.String()
}

// docMentions finds documentation nodes whose label or path matches the query
// and returns the code symbols they mention. It is the fallback for questions
// that do not name a symbol directly ("how does authentication work").
func (e *Engine) docMentions(q string, limit int) []int32 {
	terms := Terms(q)
	if len(terms) == 0 {
		return nil
	}
	type docHit struct {
		idx   int32
		score int
	}
	var docs []docHit
	inDoc := make(map[int32]bool, 4)
	for i := range e.G.Nodes {
		n := e.G.Nodes[i]
		if n.FileType == model.FileTypeCode {
			continue
		}
		hay := strings.ToLower(n.Label + " " + n.SourceFile)
		score := 0
		for _, t := range terms {
			if strings.Contains(hay, t) {
				score++
			}
		}
		if score > 0 {
			docs = append(docs, docHit{int32(i), score})
		}
	}
	if len(docs) == 0 {
		return nil
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].score != docs[j].score {
			return docs[i].score > docs[j].score
		}
		di, dj := e.G.Degree(docs[i].idx), e.G.Degree(docs[j].idx)
		if di != dj {
			return di > dj
		}
		return e.G.Nodes[docs[i].idx].Label < e.G.Nodes[docs[j].idx].Label
	})
	if len(docs) > 3 {
		docs = docs[:3]
	}
	for _, d := range docs {
		inDoc[d.idx] = true
	}
	seen := make(map[int32]bool, 8)
	var out []int32
	for i := range e.G.Edges {
		edge := e.G.Edges[i]
		if edge.Relation != "mentions" {
			continue
		}
		s := e.G.Index(edge.Source)
		t := e.G.Index(edge.Target)
		if s < 0 || t < 0 || !inDoc[s] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, int32(t))
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := e.G.Degree(out[i]), e.G.Degree(out[j])
		if di != dj {
			return di > dj
		}
		return e.G.Nodes[out[i]].Label < e.G.Nodes[out[j]].Label
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Query runs a natural-language-ish lookup and returns a textual subgraph plus
// the matched node IDs.
func (e *Engine) Query(q string, depth, tokenBudget int) (string, []string) {
	scored := e.Score(q)
	// Prefer symbols; document headings and documentation file nodes only
	// provide a starting point when the question is about a topic rather than a
	// named symbol.
	isFileNode := func(i int32) bool {
		n := e.G.Nodes[i]
		if model.IsFileExtLabel(n.Label) {
			return true
		}
		if n.SourceFile == "" {
			return false
		}
		base := path.Base(filepath.ToSlash(n.SourceFile))
		return strings.EqualFold(n.Label, base)
	}
	pick := func(accept func(i int32) bool) []int32 {
		out := make([]int32, 0, 3)
		for _, i2 := range scored {
			if accept(i2) {
				out = append(out, i2)
				if len(out) == 3 {
					break
				}
			}
		}
		return out
	}
	idx := pick(func(i int32) bool {
		return e.G.Nodes[i].FileType == model.FileTypeCode && !isFileNode(i)
	})
	if len(idx) == 0 {
		// Document headings (concepts owned by a doc) are useful seeds too.
		idx = pick(func(i int32) bool { return !isFileNode(i) })
	}
	if len(idx) == 0 {
		idx = e.docMentions(q, 3)
	}
	if len(idx) == 0 {
		return "No matching nodes found.", nil
	}
	ids := make([]string, len(idx))
	for i, i2 := range idx {
		ids[i] = e.G.Nodes[i2].ID
	}
	return e.SubgraphText(idx, depth, tokenBudget), ids
}

// Resolve finds a node by exact ID, exact label (case-insensitive) or
// case-insensitive substring, in that order.
func (e *Engine) Resolve(name string) (int32, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, false
	}
	if i := e.G.Index(name); i >= 0 {
		return int32(i), true
	}
	lower := strings.ToLower(name)
	for i := range e.G.Nodes {
		if strings.ToLower(e.G.Nodes[i].Label) == lower {
			return int32(i), true
		}
	}
	for i := range e.G.Nodes {
		if strings.Contains(strings.ToLower(e.G.Nodes[i].Label), lower) {
			return int32(i), true
		}
	}
	return -1, false
}

// Explain renders a node, its connections grouped by relation, and its
// community.
func (e *Engine) Explain(name string) (string, error) {
	idx, ok := e.Resolve(name)
	if !ok {
		return "", ErrNotFound
	}
	n := e.G.Nodes[idx]
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", n.Label)
	if n.SourceFile != "" {
		fmt.Fprintf(&b, "  source: %s %s\n", n.SourceFile, n.SourceLocation)
	}
	fmt.Fprintf(&b, "  type: %s · degree: %d · id: %s\n", n.FileType, e.G.Degree(idx), n.ID)
	if n.Community >= 0 {
		fmt.Fprintf(&b, "  community: %d\n", n.Community)
	}
	byRel := map[string][]string{}
	for _, edge := range e.G.Edges {
		var other int32
		if edge.Source == n.ID {
			other = e.G.Index(edge.Target)
		} else if edge.Target == n.ID {
			other = e.G.Index(edge.Source)
		} else {
			continue
		}
		if other < 0 {
			continue
		}
		byRel[edge.Relation] = append(byRel[edge.Relation], fmt.Sprintf("%s [%s]", e.G.Nodes[other].Label, edge.Confidence))
	}
	rels := make([]string, 0, len(byRel))
	for r := range byRel {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	for _, r := range rels {
		others := byRel[r]
		sort.Strings(others)
		fmt.Fprintf(&b, "  %s (%d): %s\n", r, len(others), strings.Join(others, ", "))
	}
	return b.String(), nil
}

// Path finds the shortest undirected path between two nodes.
func (e *Engine) Path(from, to string) ([]string, error) {
	a, ok := e.Resolve(from)
	if !ok {
		return nil, fmt.Errorf("start node not found: %s", from)
	}
	b, ok := e.Resolve(to)
	if !ok {
		return nil, fmt.Errorf("end node not found: %s", to)
	}
	if a == b {
		return []string{e.G.Nodes[a].Label}, nil
	}
	prev := make([]int32, len(e.G.Nodes))
	for i := range prev {
		prev[i] = -1
	}
	seen := make([]bool, len(e.G.Nodes))
	seen[a] = true
	queue := []int32{a}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, v := range e.G.Adj()[u] {
			if seen[v] {
				continue
			}
			seen[v] = true
			prev[v] = u
			if v == b {
				queue = nil
				break
			}
			queue = append(queue, v)
		}
	}
	if !seen[b] {
		return nil, fmt.Errorf("no path between %q and %q", from, to)
	}
	var rev []string
	for cur := b; cur >= 0; cur = prev[cur] {
		rev = append(rev, e.G.Nodes[cur].Label)
		if cur == a {
			break
		}
	}
	out := make([]string, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out, nil
}

// Neighbors returns nodes within depth hops, rendered as text.
func (e *Engine) Neighbors(name string, depth int) (string, error) {
	idx, ok := e.Resolve(name)
	if !ok {
		return "", ErrNotFound
	}
	return e.SubgraphText([]int32{idx}, depth, 2000), nil
}

// Community returns members of the community containing name.
func (e *Engine) Community(name string) (string, []string, error) {
	idx, ok := e.Resolve(name)
	if !ok {
		return "", nil, ErrNotFound
	}
	cid := e.G.Nodes[idx].Community
	var out []string
	for i := range e.G.Nodes {
		if e.G.Nodes[i].Community == cid {
			out = append(out, e.G.Nodes[i].Label)
		}
	}
	sort.Strings(out)
	return fmt.Sprintf("community %d", cid), out, nil
}

// GodNodes lists the most connected non-file nodes.
func (e *Engine) GodNodes(top int) []string {
	type pair struct {
		label string
		deg   int
	}
	var pairs []pair
	for i := range e.G.Nodes {
		node := e.G.Nodes[i]
		if e.G.IsFileNode(node) || node.SourceFile == "" {
			continue
		}
		if node.FileType != "" && node.FileType != model.FileTypeCode {
			continue
		}
		pairs = append(pairs, pair{node.Label, e.G.Degree(int32(i))})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].deg != pairs[j].deg {
			return pairs[i].deg > pairs[j].deg
		}
		return pairs[i].label < pairs[j].label
	})
	if top > 0 && len(pairs) > top {
		pairs = pairs[:top]
	}
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = fmt.Sprintf("%s (%d)", p.label, p.deg)
	}
	return out
}

// Stats summarises the graph.
func (e *Engine) Stats() string {
	return fmt.Sprintf("%d nodes · %d edges", len(e.G.Nodes), len(e.G.Edges))
}
