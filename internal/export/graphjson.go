// Package export writes the graph in every output format the reference tool
// supports: node-link JSON, a self-contained HTML viewer, Obsidian notes, wiki
// articles, Cypher, GraphML, SVG and an Obsidian canvas.
package export

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.com/neko233-com/graphify233/internal/model"
)

type nodeLink struct {
	Directed   bool              `json:"directed"`
	Multigraph bool              `json:"multigraph"`
	Graph      map[string]any    `json:"graph"`
	Nodes      []jsonNode        `json:"nodes"`
	Links      []jsonLink        `json:"links"`
	Hyperedges []model.Hyperedge `json:"hyperedges"`
}

type jsonNode struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	FileType       string `json:"file_type"`
	SourceFile     string `json:"source_file"`
	SourceLocation string `json:"source_location"`
	Community      *int   `json:"community"`
	SourceURL      string `json:"source_url,omitempty"`
	CapturedAt     string `json:"captured_at,omitempty"`
	Author         string `json:"author,omitempty"`
	Contributor    string `json:"contributor,omitempty"`
}

type jsonLink struct {
	Source          string  `json:"source"`
	Target          string  `json:"target"`
	Relation        string  `json:"relation"`
	Confidence      string  `json:"confidence"`
	SourceFile      string  `json:"source_file"`
	SourceLocation  string  `json:"source_location"`
	Weight          float64 `json:"weight"`
	Src             string  `json:"_src"`
	Tgt             string  `json:"_tgt"`
	ConfidenceScore float64 `json:"confidence_score"`
}

// ToNodeLink converts the graph to the networkx node-link shape used by the
// reference graph.json.
func ToNodeLink(g *model.Graph) nodeLink { return buildNodeLink(g, false) }

func buildNodeLink(g *model.Graph, withPositions bool) nodeLink {
	out := nodeLink{
		Graph:      map[string]any{},
		Nodes:      make([]jsonNode, 0, len(g.Nodes)),
		Links:      make([]jsonLink, 0, len(g.Edges)),
		Hyperedges: g.Hyperedges,
	}
	if out.Hyperedges == nil {
		out.Hyperedges = []model.Hyperedge{}
	}
	for _, n := range g.Nodes {
		jn := jsonNode{
			ID: n.ID, Label: n.Label, FileType: n.FileType,
			SourceFile: n.SourceFile, SourceLocation: n.SourceLocation,
			SourceURL: n.SourceURL, CapturedAt: n.CapturedAt,
			Author: n.Author, Contributor: n.Contributor,
		}
		if n.Community >= 0 {
			c := n.Community
			jn.Community = &c
		}
		out.Nodes = append(out.Nodes, jn)
	}
	for _, e := range g.Edges {
		out.Links = append(out.Links, jsonLink{
			Source: e.Source, Target: e.Target,
			Relation: e.Relation, Confidence: e.Confidence,
			SourceFile: e.SourceFile, SourceLocation: e.SourceLocation,
			Weight: e.Weight, Src: e.Source, Tgt: e.Target,
			ConfidenceScore: e.ConfidenceScore,
		})
	}
	return out
}

// WriteJSON writes graph.json (indented, streaming encoder).
func WriteJSON(path string, g *model.Graph) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	if err := EncodeJSON(w, g); err != nil {
		return err
	}
	return w.Flush()
}

// EncodeJSON streams the node-link JSON.
func EncodeJSON(w io.Writer, g *model.Graph) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(ToNodeLink(g))
}

// LoadJSON reads a graph.json produced by this tool (or the reference).
func LoadJSON(path string) (*model.Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw nodeLink
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	g := model.New()
	g.Nodes = make([]*model.Node, 0, len(raw.Nodes))
	for _, n := range raw.Nodes {
		node := &model.Node{
			ID: n.ID, Label: n.Label, FileType: n.FileType,
			SourceFile: n.SourceFile, SourceLocation: n.SourceLocation,
			Community: -1, SourceURL: n.SourceURL, CapturedAt: n.CapturedAt,
			Author: n.Author, Contributor: n.Contributor,
		}
		if n.Community != nil {
			node.Community = *n.Community
		}
		g.Nodes = append(g.Nodes, node)
	}
	g.EnsureIndex()
	for _, l := range raw.Links {
		g.AddEdge(model.Edge{
			Source: l.Source, Target: l.Target, Relation: l.Relation,
			Confidence: l.Confidence, SourceFile: l.SourceFile,
			SourceLocation: l.SourceLocation, Weight: l.Weight,
			ConfidenceScore: l.ConfidenceScore,
		})
	}
	g.Hyperedges = raw.Hyperedges
	return g, nil
}
