// Package model defines the core knowledge-graph data structures.
//
// The representation is deliberately close to the Python reference's
// networkx node-link output so existing graph.json consumers keep working,
// while the internal form is index based (int32) to keep traversal fast and
// allocation free on hot paths.
package model

import (
	"sort"
	"strings"
)

// FileType is the string tag stored on every node.
type FileType = string

// FileType classifies a corpus file / node origin.
const (
	FileTypeCode     = "code"
	FileTypeDocument = "document"
	FileTypePaper    = "paper"
	FileTypeImage    = "image"
)

// Confidence levels attached to every edge. The graph never invents an edge
// without labelling how it was found.
const (
	ConfExtracted = "EXTRACTED"
	ConfInferred  = "INFERRED"
	ConfAmbiguous = "AMBIGUOUS"
)

// DefaultConfidenceScore mirrors the reference export defaults.
func DefaultConfidenceScore(conf string) float64 {
	switch conf {
	case ConfInferred:
		return 0.5
	case ConfAmbiguous:
		return 0.2
	default:
		return 1.0
	}
}

// Node is a graph vertex.
type Node struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	FileType       string `json:"file_type"`
	SourceFile     string `json:"source_file"`
	SourceLocation string `json:"source_location"`

	// Community is -1 until clustering assigns a community. Export writes the
	// field only when assigned so stub/external nodes stay unlabelled.
	Community int `json:"-"`

	// Optional provenance fields produced by semantic/doc ingestion.
	SourceURL   string `json:"source_url,omitempty"`
	CapturedAt  string `json:"captured_at,omitempty"`
	Author      string `json:"author,omitempty"`
	Contributor string `json:"contributor,omitempty"`
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	Source          string  `json:"source"`
	Target          string  `json:"target"`
	Relation        string  `json:"relation"`
	Confidence      string  `json:"confidence"`
	SourceFile      string  `json:"source_file"`
	SourceLocation  string  `json:"source_location"`
	Weight          float64 `json:"weight"`
	ConfidenceScore float64 `json:"confidence_score"`
}

// Hyperedge is a group relationship (three or more participants).
type Hyperedge struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Nodes           []string `json:"nodes"`
	Relation        string   `json:"relation"`
	Confidence      string   `json:"confidence"`
	ConfidenceScore float64  `json:"confidence_score,omitempty"`
	SourceFile      string   `json:"source_file,omitempty"`
}

// Extraction is the raw per-run extraction payload (cacheable, JSON friendly).
type Extraction struct {
	Nodes        []Node      `json:"nodes"`
	Edges        []Edge      `json:"edges"`
	Hyperedges   []Hyperedge `json:"hyperedges,omitempty"`
	InputTokens  int         `json:"input_tokens"`
	OutputTokens int         `json:"output_tokens"`
	Error        string      `json:"error,omitempty"`
}

// Graph is an undirected simple graph with node attributes. Parallel edges
// collapse onto one undirected pair, matching networkx.Graph semantics; the
// most recently added edge wins (same as G.add_edge updating attrs).
type Graph struct {
	Nodes      []*Node
	Edges      []*Edge
	Hyperedges []Hyperedge

	index    map[string]int32
	edgeIdx  map[edgeKey]int32
	adj      [][]int32
	adjDirty bool

	// relationIds interns relation strings for fast grouping in exports.
	relationIds map[string]int
}

type edgeKey struct{ a, b int32 }

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		index:       make(map[string]int32, 64),
		edgeIdx:     make(map[edgeKey]int32, 64),
		relationIds: make(map[string]int, 8),
	}
}

// Len returns the node count.
func (g *Graph) Len() int { return len(g.Nodes) }

// EdgeCount returns the deduplicated undirected edge count.
func (g *Graph) EdgeCount() int { return len(g.Edges) }

// Index returns the dense index for a node id, or -1.
func (g *Graph) Index(id string) int32 {
	if i, ok := g.index[id]; ok {
		return i
	}
	return -1
}

// AddNode inserts a node or merges attributes into an existing one.
func (g *Graph) AddNode(n Node) int32 {
	if i, ok := g.index[n.ID]; ok {
		dst := g.Nodes[i]
		if dst.Label == "" {
			dst.Label = n.Label
		}
		if dst.FileType == "" {
			dst.FileType = n.FileType
		}
		if dst.SourceFile == "" {
			dst.SourceFile = n.SourceFile
		}
		if dst.SourceLocation == "" {
			dst.SourceLocation = n.SourceLocation
		}
		if dst.SourceURL == "" {
			dst.SourceURL = n.SourceURL
		}
		if dst.CapturedAt == "" {
			dst.CapturedAt = n.CapturedAt
		}
		if dst.Author == "" {
			dst.Author = n.Author
		}
		if dst.Contributor == "" {
			dst.Contributor = n.Contributor
		}
		return i
	}
	nn := n
	g.Nodes = append(g.Nodes, &nn)
	i := int32(len(g.Nodes) - 1)
	g.index[nn.ID] = i
	g.adjDirty = true
	return i
}

// EnsureIndex preserves the ability to look up nodes added directly to Nodes.
func (g *Graph) EnsureIndex() {
	if g.index == nil {
		g.index = make(map[string]int32, len(g.Nodes))
	}
	if len(g.index) == len(g.Nodes) {
		return
	}
	for i, n := range g.Nodes {
		g.index[n.ID] = int32(i)
	}
	g.adjDirty = true
}

// HasEdge reports whether an undirected edge already connects a and b.
func (g *Graph) HasEdge(a, b string) bool {
	g.EnsureIndex()
	ia, ok := g.index[a]
	if !ok {
		return false
	}
	ib, ok := g.index[b]
	if !ok {
		return false
	}
	if ia > ib {
		ia, ib = ib, ia
	}
	if g.edgeIdx == nil {
		return false
	}
	_, ok = g.edgeIdx[edgeKey{ia, ib}]
	return ok
}

// structuralRel marks edges that describe containment or imports rather than a
// dependency between two symbols.
var structuralRel = map[string]bool{
	"contains": true, "method": true, "imports": true, "imports_from": true,
}

// AddEdge adds an edge if both endpoints exist, collapsing parallel pairs.
func (g *Graph) AddEdge(e Edge) bool {
	g.EnsureIndex()
	s, ok := g.index[e.Source]
	if !ok {
		return false
	}
	t, ok := g.index[e.Target]
	if !ok {
		return false
	}
	if e.Weight == 0 {
		e.Weight = 1
	}
	if e.ConfidenceScore == 0 {
		e.ConfidenceScore = DefaultConfidenceScore(e.Confidence)
	}
	if g.edgeIdx == nil {
		g.edgeIdx = make(map[edgeKey]int32, len(g.Edges))
	}
	key := edgeKey{s, t}
	if s > t {
		key = edgeKey{t, s}
	}
	if idx, ok := g.edgeIdx[key]; ok {
		// Later extraction wins, mirroring networkx attribute updates, except
		// that structural edges never overwrite a real dependency edge (a file
		// "contains" a symbol must not erase an observed "calls" edge).
		if structuralRel[g.Edges[idx].Relation] && !structuralRel[e.Relation] {
			g.Edges[idx] = &e
			return true
		}
		if !structuralRel[g.Edges[idx].Relation] && structuralRel[e.Relation] {
			return true
		}
		g.Edges[idx] = &e
		return true
	}
	g.Edges = append(g.Edges, &e)
	g.edgeIdx[key] = int32(len(g.Edges) - 1)
	g.adjDirty = true
	return true
}

// BuildAdjacency materialises a CSR-ish neighbour list, deduplicated.
func (g *Graph) BuildAdjacency() [][]int32 {
	if !g.adjDirty && g.adj != nil {
		return g.adj
	}
	g.EnsureIndex()
	deg := make([]int32, len(g.Nodes))
	for _, e := range g.Edges {
		s, ok1 := g.index[e.Source]
		t, ok2 := g.index[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		deg[s]++
		if s != t {
			deg[t]++
		}
	}
	adj := make([][]int32, len(g.Nodes))
	for i, d := range deg {
		if d > 0 {
			adj[i] = make([]int32, 0, d)
		}
	}
	for _, e := range g.Edges {
		s, ok1 := g.index[e.Source]
		t, ok2 := g.index[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		adj[s] = append(adj[s], t)
		if s != t {
			adj[t] = append(adj[t], s)
		}
	}
	g.adj = adj
	g.adjDirty = false
	return adj
}

// Adj returns the neighbour list (cached).
func (g *Graph) Adj() [][]int32 { return g.BuildAdjacency() }

// Degree returns the number of distinct neighbours (self loops count twice).
func (g *Graph) Degree(i int32) int { return len(g.Adj()[i]) }

// RemoveIsolatedCodeNodes drops code file nodes with no edges, matching the
// reference build step.
func (g *Graph) RemoveIsolatedCodeNodes() int {
	adj := g.BuildAdjacency()
	keep := make([]bool, len(g.Nodes))
	removed := 0
	for i, n := range g.Nodes {
		if len(adj[i]) == 0 && n.FileType == FileTypeCode {
			keep[i] = false
			removed++
			continue
		}
		keep[i] = true
	}
	if removed == 0 {
		return 0
	}
	g.compact(keep)
	return removed
}

func (g *Graph) compact(keep []bool) {
	newIndex := make([]int32, len(g.Nodes))
	nodes := make([]*Node, 0, len(g.Nodes)-1)
	for i, ok := range keep {
		if ok {
			newIndex[i] = int32(len(nodes))
			nodes = append(nodes, g.Nodes[i])
		} else {
			newIndex[i] = -1
		}
	}
	edges := g.Edges[:0]
	edgeIdx := make(map[edgeKey]int32, len(g.Edges))
	for _, e := range g.Edges {
		s, ok1 := g.index[e.Source]
		t, ok2 := g.index[e.Target]
		if !ok1 || !ok2 {
			continue
		}
		ns, nt := newIndex[s], newIndex[t]
		if ns < 0 || nt < 0 {
			continue
		}
		key := edgeKey{ns, nt}
		if ns > nt {
			key = edgeKey{nt, ns}
		}
		if idx, ok := edgeIdx[key]; ok {
			edges[idx] = e
			continue
		}
		edges = append(edges, e)
		edgeIdx[key] = int32(len(edges) - 1)
	}
	g.Nodes = nodes
	g.Edges = edges
	g.index = make(map[string]int32, len(nodes))
	for i, n := range nodes {
		g.index[n.ID] = int32(i)
	}
	g.edgeIdx = edgeIdx
	g.adjDirty = true
}

// SortNodesByID provides deterministic output independent of walk order.
func (g *Graph) SortNodesByID() {
	sort.SliceStable(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	g.index = nil
	g.EnsureIndex()
}

// RelationID interns a relation name.
func (g *Graph) RelationID(rel string) int {
	if g.relationIds == nil {
		g.relationIds = make(map[string]int, 8)
	}
	if id, ok := g.relationIds[rel]; ok {
		return id
	}
	id := len(g.relationIds)
	g.relationIds[rel] = id
	return id
}

// IsConceptNode reports whether a node represents an abstract concept rather
// than a concrete code entity (mirrors the reference heuristic).
func IsConceptNode(n *Node) bool {
	if n.SourceFile == "" {
		return true
	}
	last := n.SourceFile
	if i := strings.LastIndexAny(last, `/\`); i >= 0 {
		last = last[i+1:]
	}
	return !strings.Contains(last, ".")
}

var codeExtSet = map[string]bool{
	"py": true, "ts": true, "tsx": true, "js": true, "jsx": true, "go": true,
	"rs": true, "java": true, "rb": true, "cpp": true, "cc": true, "cxx": true,
	"c": true, "h": true, "hpp": true, "cs": true, "kt": true, "kts": true,
	"scala": true, "php": true, "swift": true,
}

func extOf(label string) string {
	i := strings.LastIndexByte(label, '.')
	if i < 0 || i == len(label)-1 {
		return ""
	}
	return strings.ToLower(label[i+1:])
}

// IsFileNode mirrors analyze._is_file_node: a node that represents a file
// rather than a symbol, a method label, or a leaf call.
func (g *Graph) IsFileNode(n *Node) bool {
	label := n.Label
	e := extOf(label)
	if codeExtSet[e] {
		return true
	}
	if strings.HasPrefix(label, ".") && strings.HasSuffix(label, "()") {
		return true
	}
	if strings.HasSuffix(label, "()") && g.Degree(g.Index(n.ID)) <= 1 {
		return true
	}
	return false
}

// IsFileLabel is the label-only variant used before a graph exists.
func IsFileLabel(label string) bool {
	if codeExtSet[extOf(label)] {
		return true
	}
	return strings.HasPrefix(label, ".") && strings.HasSuffix(label, "()")
}

// IsFileExtLabel reports whether the label is a filename with a known code
// extension ("a.py"). Unlike IsFileLabel it does not treat method labels like
// ".start()" as files, so symbol matching still sees them.
func IsFileExtLabel(label string) bool { return codeExtSet[extOf(label)] }
