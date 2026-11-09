// Package analyze derives the insights the reference report is built from:
// god nodes, surprising connections, community cohesion and suggested
// questions. Everything here is deterministic for a given graph.
package analyze

import (
	"sort"
	"strconv"
	"strings"

	"github.com/neko233-com/graphify233/internal/cluster"
	"github.com/neko233-com/graphify233/internal/model"
)

// GodNode is a highly connected node.
type GodNode struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Edges      int    `json:"edges"`
	SourceFile string `json:"source_file"`
}

// Surprise is an edge worth a second look.
type Surprise struct {
	Source      string   `json:"source"`
	SourceLabel string   `json:"source_label"`
	Target      string   `json:"target"`
	TargetLabel string   `json:"target_label"`
	Relation    string   `json:"relation"`
	Confidence  string   `json:"confidence"`
	Score       float64  `json:"score"`
	Why         []string `json:"why"`
	SourceFile  string   `json:"source_file"`
	TargetFile  string   `json:"target_file"`
}

// GodNodes returns the most connected non-file, non-concept nodes.
func GodNodes(g *model.Graph, topN int) []GodNode {
	type cand struct {
		idx int32
		deg int
	}
	cands := make([]cand, 0, len(g.Nodes))
	for i, n := range g.Nodes {
		idx := int32(i)
		if g.IsFileNode(n) || model.IsConceptNode(n) {
			continue
		}
		cands = append(cands, cand{idx: idx, deg: len(g.Adj()[idx])})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].deg != cands[j].deg {
			return cands[i].deg > cands[j].deg
		}
		return g.Nodes[cands[i].idx].ID < g.Nodes[cands[j].idx].ID
	})
	if topN > len(cands) {
		topN = len(cands)
	}
	out := make([]GodNode, 0, topN)
	for i := 0; i < topN; i++ {
		n := g.Nodes[cands[i].idx]
		out = append(out, GodNode{ID: n.ID, Label: n.Label, Edges: cands[i].deg, SourceFile: n.SourceFile})
	}
	return out
}

// CohesionScores returns the cohesion for each community, in the given order.
func CohesionScores(g *model.Graph, comms [][]int32) []float64 {
	stamp := make([]int32, len(g.Nodes))
	out := make([]float64, len(comms))
	adj := g.Adj()
	for i, c := range comms {
		out[i] = cluster.Cohesion(c, adj, stamp, int32(i+1))
	}
	return out
}

var structuralRelations = map[string]bool{
	"imports": true, "imports_from": true, "contains": true, "method": true,
}

func confRank(c string) int {
	switch c {
	case model.ConfAmbiguous:
		return 3
	case model.ConfInferred:
		return 2
	default:
		return 1
	}
}

// SurprisingConnections ranks edges the reader probably did not know about.
// When the corpus spans several files it favours cross-file edges, otherwise
// it falls back to edges that bridge communities.
func SurprisingConnections(g *model.Graph, comms [][]int32, topN int) []Surprise {
	files := make(map[string]struct{}, 16)
	for _, n := range g.Nodes {
		if n.SourceFile != "" {
			files[n.SourceFile] = struct{}{}
		}
	}
	if len(files) > 1 {
		if out := crossFileSurprises(g, topN); len(out) > 0 {
			return out
		}
	}
	return crossCommunitySurprises(g, comms, topN)
}

func crossFileSurprises(g *model.Graph, topN int) []Surprise {
	out := make([]Surprise, 0, 32)
	adj := g.Adj()
	for _, e := range g.Edges {
		if structuralRelations[e.Relation] {
			continue
		}
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		sn, tn := g.Nodes[s], g.Nodes[t]
		if sn.SourceFile == "" || tn.SourceFile == "" || sn.SourceFile == tn.SourceFile {
			continue
		}
		if g.IsFileNode(sn) || g.IsFileNode(tn) || model.IsConceptNode(sn) || model.IsConceptNode(tn) {
			continue
		}
		score := float64(confRank(e.Confidence))
		var why []string
		if sn.FileType != tn.FileType {
			score += 2
			why = append(why, "crosses file types ("+sn.FileType+" → "+tn.FileType+")")
		}
		if topDir(sn.SourceFile) != topDir(tn.SourceFile) {
			score += 2
			why = append(why, "different top-level directories")
		}
		if sn.Community >= 0 && tn.Community >= 0 && sn.Community != tn.Community {
			score += 1
			why = append(why, "cross-community")
		}
		if e.Relation == "semantically_similar_to" {
			score *= 1.5
			why = append(why, "semantic similarity")
		}
		ds, dt := len(adj[s]), len(adj[t])
		if ds < dt {
			ds, dt = dt, ds
		}
		if dt <= 2 && ds >= 5 {
			score += 1
			why = append(why, "peripheral node reaches a hub")
		}
		if len(why) == 0 {
			why = append(why, "spans files")
		}
		out = append(out, Surprise{
			Source: e.Source, SourceLabel: sn.Label,
			Target: e.Target, TargetLabel: tn.Label,
			Relation: e.Relation, Confidence: e.Confidence,
			Score: score, Why: why,
			SourceFile: sn.SourceFile, TargetFile: tn.SourceFile,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].SourceLabel != out[j].SourceLabel {
			return out[i].SourceLabel < out[j].SourceLabel
		}
		return out[i].TargetLabel < out[j].TargetLabel
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

func crossCommunitySurprises(g *model.Graph, comms [][]int32, topN int) []Surprise {
	if len(comms) == 0 {
		bc := Betweenness(g, 0)
		type edgeBC struct {
			e    *model.Edge
			score float64
		}
		ranked := make([]edgeBC, 0, len(g.Edges))
		for _, e := range g.Edges {
			s := g.Index(e.Source)
			t := g.Index(e.Target)
			if s < 0 || t < 0 {
				continue
			}
			ranked = append(ranked, edgeBC{e: e, score: (bc[s] + bc[t]) / 2})
		}
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
		out := make([]Surprise, 0, topN)
		for i := 0; i < len(ranked) && i < topN; i++ {
			e := ranked[i].e
			s := g.Nodes[g.Index(e.Source)]
			t := g.Nodes[g.Index(e.Target)]
			out = append(out, Surprise{
				Source: e.Source, SourceLabel: s.Label,
				Target: e.Target, TargetLabel: t.Label,
				Relation: e.Relation, Confidence: e.Confidence,
				Score: ranked[i].score,
				Why:   []string{"bridges graph structure"},
				SourceFile: s.SourceFile, TargetFile: t.SourceFile,
			})
		}
		return out
	}

	type cand struct {
		e    *model.Edge
		s, t int32
	}
	cands := make([]cand, 0, len(g.Edges))
	for _, e := range g.Edges {
		if structuralRelations[e.Relation] {
			continue
		}
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		if g.Nodes[s].Community == g.Nodes[t].Community {
			continue
		}
		if g.IsFileNode(g.Nodes[s]) || g.IsFileNode(g.Nodes[t]) {
			continue
		}
		cands = append(cands, cand{e: e, s: s, t: t})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := confRank(cands[i].e.Confidence), confRank(cands[j].e.Confidence)
		if ri != rj {
			return ri > rj
		}
		return cands[i].e.Source < cands[j].e.Source
	})
	seen := make(map[[2]int]struct{}, 8)
	out := make([]Surprise, 0, topN)
	for _, c := range cands {
		cs, ct := g.Nodes[c.s].Community, g.Nodes[c.t].Community
		if cs > ct {
			cs, ct = ct, cs
		}
		key := [2]int{cs, ct}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		sn, tn := g.Nodes[c.s], g.Nodes[c.t]
		out = append(out, Surprise{
			Source: c.e.Source, SourceLabel: sn.Label,
			Target: c.e.Target, TargetLabel: tn.Label,
			Relation: c.e.Relation, Confidence: c.e.Confidence,
			Score: float64(confRank(c.e.Confidence)),
			Why:   []string{"bridges communities"},
			SourceFile: sn.SourceFile, TargetFile: tn.SourceFile,
		})
		if len(out) >= topN {
			break
		}
	}
	return out
}

func topDir(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// SuggestedQuestions proposes follow-up questions grounded in graph signals.
func SuggestedQuestions(g *model.Graph, comms [][]int32, cohesion []float64, labels map[int]string) []string {
	out := make([]string, 0, 8)

	for _, e := range g.Edges {
		if e.Confidence != model.ConfAmbiguous {
			continue
		}
		s, t := g.Index(e.Source), g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		out = append(out, "What does \""+e.Relation+"\" mean between `"+g.Nodes[s].Label+"` and `"+g.Nodes[t].Label+"`?")
		if len(out) >= 2 {
			break
		}
	}

	bc := Betweenness(g, 0)
	type bridge struct {
		idx   int32
		score float64
	}
	bridges := make([]bridge, 0, len(g.Nodes))
	for i := range g.Nodes {
		if bc[i] > 0 {
			bridges = append(bridges, bridge{idx: int32(i), score: bc[i]})
		}
	}
	sort.SliceStable(bridges, func(i, j int) bool {
		if bridges[i].score != bridges[j].score {
			return bridges[i].score > bridges[j].score
		}
		return g.Nodes[bridges[i].idx].ID < g.Nodes[bridges[j].idx].ID
	})
	for i := 0; i < len(bridges) && i < 3; i++ {
		n := g.Nodes[bridges[i].idx]
		out = append(out, "How does `"+n.Label+"` connect the different parts of the system (bridges "+describeCommunities(comms, labels, g, bridges[i].idx)+")?")
	}

	adj := g.Adj()
	type hub struct {
		idx    int32
		deg    int
		infer  int
	}
	hubs := make([]hub, 0, 8)
	inferredCount := make(map[int32]int, 16)
	for _, e := range g.Edges {
		if e.Confidence != model.ConfInferred {
			continue
		}
		if s := g.Index(e.Source); s >= 0 {
			inferredCount[s]++
		}
		if t := g.Index(e.Target); t >= 0 {
			inferredCount[t]++
		}
	}
	for i := range g.Nodes {
		deg := len(adj[i])
		if deg == 0 || inferredCount[int32(i)] < 2 {
			continue
		}
		hubs = append(hubs, hub{idx: int32(i), deg: deg, infer: inferredCount[int32(i)]})
	}
	sort.SliceStable(hubs, func(i, j int) bool {
		if hubs[i].deg != hubs[j].deg {
			return hubs[i].deg > hubs[j].deg
		}
		return g.Nodes[hubs[i].idx].ID < g.Nodes[hubs[j].idx].ID
	})
	for i := 0; i < len(hubs) && i < 2; i++ {
		n := g.Nodes[hubs[i].idx]
		out = append(out, "Are the "+itoa(hubs[i].infer)+" inferred relationships involving `"+n.Label+"` correct?")
	}

	for i := range g.Nodes {
		if len(adj[i]) == 0 {
			out = append(out, "What is the role of `"+g.Nodes[i].Label+"`? It has no connections.")
			if len(out) >= 7 {
				break
			}
		}
	}

	for i, c := range comms {
		if len(c) < 5 || i >= len(cohesion) {
			continue
		}
		if cohesion[i] < 0.15 {
			name := labels[i]
			if name == "" {
				name = "community " + itoa(i)
			}
			out = append(out, "Is \""+name+"\" really one module? Cohesion is only "+format2(cohesion[i])+".")
		}
	}

	if len(out) == 0 {
		out = append(out, "No strong signal found - the graph is small or sparsely connected.")
	}
	if len(out) > 7 {
		out = out[:7]
	}
	return out
}

func describeCommunities(comms [][]int32, labels map[int]string, g *model.Graph, idx int32) string {
	di := -1
	for ci, c := range comms {
		for _, m := range c {
			if m == idx {
				di = ci
				break
			}
		}
		if di >= 0 {
			break
		}
	}
	var names []string
	for ci, c := range comms {
		if ci == di {
			continue
		}
		for _, m := range c {
			if len(g.Adj()[m]) > 0 {
				name := labels[ci]
				if name == "" {
					name = "community " + itoa(ci)
				}
				names = append(names, name)
				break
			}
		}
		if len(names) >= 2 {
			break
		}
	}
	if len(names) == 0 {
		return "multiple parts"
	}
	return strings.Join(names, " and ")
}

// LabelCommunities derives readable community names from their strongest
// member and dominant source directory.
func LabelCommunities(g *model.Graph, comms [][]int32) map[int]string {
	out := make(map[int]string, len(comms))
	for ci, c := range comms {
		var best int32 = -1
		bestDeg := -1
		dirs := make(map[string]int, 8)
		for _, m := range c {
			if deg := len(g.Adj()[m]); deg > bestDeg {
				bestDeg = deg
				best = m
			}
			if f := g.Nodes[m].SourceFile; f != "" {
				dirs[topDir(f)]++
			}
		}
		label := ""
		if best >= 0 {
			label = strings.TrimSpace(g.Nodes[best].Label)
		}
		if label == "" {
			label = "Community " + itoa(ci)
		}
		out[ci] = label
	}
	return out
}

// Betweenness computes (sampled) node betweenness centrality on the
// undirected unweighted graph. samples <= 0 selects an automatic sample size.
func Betweenness(g *model.Graph, samples int) []float64 {
	n := len(g.Nodes)
	bc := make([]float64, n)
	if n == 0 {
		return bc
	}
	adj := g.Adj()
	edges := len(g.Edges)
	if samples <= 0 {
		samples = n
		if n > 2000 {
			samples = 256
		}
		if edges > 0 && int64(n)*int64(edges) > 40_000_000 {
			samples = 128
		}
	}
	if samples > n {
		samples = n
	}

	sigma := make([]float64, n)
	dist := make([]int, n)
	delta := make([]float64, n)
	stack := make([]int32, 0, n)
	queue := make([]int32, 0, n)
	paths := make(map[int32][]int32, n)

	step := 1
	if samples > 0 {
		step = n / samples
		if step < 1 {
			step = 1
		}
	}
	processed := 0
	for s := 0; s < n; s += step {
		for i := range sigma {
			sigma[i] = 0
			dist[i] = -1
			delta[i] = 0
		}
		stack = stack[:0]
		queue = queue[:0]
		for k := range paths {
			delete(paths, k)
		}
		sigma[s] = 1
		dist[s] = 0
		queue = append(queue, int32(s))
		for qi := 0; qi < len(queue); qi++ {
			v := queue[qi]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					paths[w] = append(paths[w], v)
				}
			}
		}
		for si := len(stack) - 1; si >= 0; si-- {
			w := stack[si]
			for _, v := range paths[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != int32(s) {
				bc[w] += delta[w]
			}
		}
		processed++
	}
	if processed == 0 {
		return bc
	}
	scale := float64(n) / float64(processed)
	for i := range bc {
		bc[i] *= scale
	}
	return bc
}

func itoa(v int) string { return strconv.Itoa(v) }

func format2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
