// Package pipeline wires detection, extraction, graph building, clustering,
// analysis and every output format into one deterministic run.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neko233-com/graphify233/internal/analyze"
	"github.com/neko233-com/graphify233/internal/cache"
	"github.com/neko233-com/graphify233/internal/cluster"
	"github.com/neko233-com/graphify233/internal/detect"
	"github.com/neko233-com/graphify233/internal/enrich"
	"github.com/neko233-com/graphify233/internal/export"
	"github.com/neko233-com/graphify233/internal/extract"
	"github.com/neko233-com/graphify233/internal/graphbuild"
	"github.com/neko233-com/graphify233/internal/model"
	"github.com/neko233-com/graphify233/internal/report"
	"github.com/neko233-com/graphify233/internal/semantic"
)

// Formats selects which artifacts a run writes.
type Formats struct {
	JSON     bool
	Report   bool
	HTML     bool
	Obsidian bool
	Wiki     bool
	SVG      bool
	GraphML  bool
	Cypher   bool
	Canvas   bool
	// ObsidianForced overrides the node cap that protects huge corpora from
	// writing one Markdown file per node.
	ObsidianForced bool
}

// MaxObsidianNodes caps one-note-per-node output, which is one file per node.
const MaxObsidianNodes = 20000

// DefaultFormats matches the reference defaults: JSON, report, HTML and
// Obsidian. Wiki/SVG/GraphML/Cypher/Canvas are opt-in.
func DefaultFormats() Formats {
	return Formats{JSON: true, Report: true, HTML: true, Obsidian: true}
}

// Options controls a pipeline run.
type Options struct {
	Root    string
	OutDir  string
	Mode    string // "standard" or "deep"
	Update  bool
	Workers int
	Formats Formats
	Neo4j   *export.Neo4jConfig
	Label   string // optional override for the report root label
	Logf    func(format string, args ...any)

	// Optional semantic layer. Concepts similarity is deterministic; the
	// embeddings layer runs offline with SemanticLocal or against an
	// OpenAI-compatible endpoint when SemanticURL is set.
	Semantic          bool
	SemanticLocal     bool
	SemanticURL       string
	SemanticModel     string
	SemanticKey       string
	SemanticThreshold float64
}

// Result carries everything a caller (CLI, MCP, watch) needs after a run.
type Result struct {
	Root            string
	OutDir          string
	Graph           *model.Graph
	Communities     [][]int32
	Labels          map[int]string
	Cohesion        []float64
	Gods            []analyze.GodNode
	Surprises       []analyze.Surprise
	Questions       []string
	Detection       *detect.Result
	FilesExtract    int
	FilesCached     int
	FilesFailed     int
	InputTokens     int
	OutputTokens    int
	Elapsed         time.Duration
	DetectTime      time.Duration
	ExtractTime     time.Duration
	ClusterTime     time.Duration
	WriteTime       time.Duration
	Outputs         []string
	Neo4jSent       int
	Mentions        int
	Concepts        int
	ConceptMentions int
	Semantic        int
	Warnings        []string
}

type fileJob struct {
	idx  int
	path string
	kind model.FileType
	rel  string
}

// Run executes the full pipeline.
func Run(opts Options) (*Result, error) {
	start := time.Now()
	if opts.Root == "" {
		opts.Root = "."
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(opts.Root, "graphify-out")
	}
	if opts.Mode == "" {
		opts.Mode = "standard"
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	logf("Detecting files under %s", opts.Root)
	det, err := detect.Detect(opts.Root)
	if err != nil {
		return nil, err
	}
	if det.TotalFiles == 0 {
		return nil, fmt.Errorf("no supported files found under %s", opts.Root)
	}
	detectDur := time.Since(start)

	jobs := make([]fileJob, 0, det.TotalFiles)
	for kind, paths := range det.Files {
		for _, p := range paths {
			jobs = append(jobs, fileJob{path: p, kind: string(kind)})
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].path < jobs[j].path })
	for i := range jobs {
		jobs[i].idx = i
		jobs[i].rel = displayRel(opts.Root, jobs[i].path)
	}

	old := cache.LoadManifest(opts.Root)
	// Cached extractions are only valid for the extractor format that produced
	// them; a version bump (schema or semantics) invalidates the whole cache.
	toolTag := fmt.Sprintf("graphify233/extract-%d", extract.Version)
	if old.Tool != "" && old.Tool != toolTag {
		if _, err := cache.Clear(opts.Root); err != nil && opts.Logf != nil {
			opts.Logf("cache: %v", err)
		}
		old = &cache.Manifest{}
		opts.Update = false
	}
	if opts.Update && old.Files == nil {
		opts.Update = false
	}
	results := make([]model.Extraction, len(jobs))
	var extracted, cached, failed int64

	var wg sync.WaitGroup
	sem := make(chan struct{}, opts.Workers)
	for i := range jobs {
		job := jobs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Fast path: unchanged according to the manifest.
			if opts.Update {
				if e, ok := old.Files[job.rel]; ok {
					if fi, err := os.Stat(job.path); err == nil && fi.Size() == e.Size && fi.ModTime().UnixNano() == e.ModTime {
						if ex, ok := cache.Get(opts.Root, e.Digest); ok {
							results[job.idx] = *ex
							atomic.AddInt64(&cached, 1)
							return
						}
					}
				}
			}
			digest, err := cache.HashFile(job.path)
			if err != nil {
				atomic.AddInt64(&failed, 1)
				return
			}
			if ex, ok := cache.Get(opts.Root, digest); ok && job.kind == model.FileTypeCode {
				results[job.idx] = *ex
				atomic.AddInt64(&cached, 1)
				return
			}
			ex := extract.File(job.path, job.kind)
			if ex.Error == "" {
				_ = cache.Put(opts.Root, digest, &ex)
			}
			results[job.idx] = ex
			atomic.AddInt64(&extracted, 1)
		}()
	}
	wg.Wait()
	extractDur := time.Since(start) - detectDur

	exs := make([]model.Extraction, 0, len(results))
	for i := range results {
		exs = append(exs, results[i])
	}
	g := graphbuild.FromExtractions(exs)

	// Deep mode links documentation to code by matching symbol names. It is
	// deterministic and local: no LLM, no network.
	mentions := 0
	concepts := 0
	conceptMentions := 0
	if opts.Mode == "deep" {
		var docs []enrich.Doc
		for i := range jobs {
			switch jobs[i].kind {
			case string(detect.Document), string(detect.Paper):
				docs = append(docs, enrich.Doc{Path: jobs[i].path, Rel: jobs[i].rel, Kind: jobs[i].kind})
			}
		}
		mentions = enrich.Mentions(g, docs, enrich.Limits{})
		concepts, conceptMentions = enrich.Concepts(g, docs, enrich.Limits{})
	}
	semanticEdges := 0
	var semanticWarn string
	if opts.Semantic {
		semanticEdges = semantic.Concepts(g, 0.5, semantic.Limits{})
		var embedder semantic.Embedder
		switch {
		case opts.SemanticLocal:
			embedder = semantic.HashingEmbedder{Dim: 1024}
		case opts.SemanticURL != "":
			embedder = &semantic.EmbeddingClient{URL: opts.SemanticURL, Model: opts.SemanticModel, APIKey: opts.SemanticKey}
		}
		if embedder != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			n, err := semantic.EmbeddingLinks(ctx, g, embedder, opts.SemanticThreshold, semantic.Limits{})
			cancel()
			if err != nil {
				semanticWarn = "semantic embeddings skipped: " + err.Error()
			} else {
				semanticEdges += n
			}
		}
	}
	g.BuildAdjacency()
	logf("Graph: %d nodes, %d edges", len(g.Nodes), len(g.Edges))

	comms := cluster.Detect(len(g.Nodes), g.Adj())
	// Renumber by anchor node so community ids stay stable across re-runs;
	// Detect's size ordering would shift every id when one file is added.
	ids := make([]string, len(g.Nodes))
	degs := make([]int32, len(g.Nodes))
	for i, n := range g.Nodes {
		ids[i] = n.ID
		degs[i] = int32(g.Degree(int32(i)))
	}
	comms = cluster.StableOrder(comms, ids, degs)
	for ci, c := range comms {
		for _, m := range c {
			g.Nodes[m].Community = ci
		}
	}
	cohesion := analyze.CohesionScores(g, comms)
	labels := analyze.LabelCommunities(g, comms)
	godN, surpriseN := 10, 5
	if opts.Mode == "deep" {
		// The Go build never calls an LLM; "deep" widens the deterministic
		// analysis instead of adding a semantic extraction pass.
		godN, surpriseN = 20, 15
	}
	gods := analyze.GodNodes(g, godN)
	surprises := analyze.SurprisingConnections(g, comms, surpriseN)
	questions := analyze.SuggestedQuestions(g, comms, cohesion, labels)
	clusterDur := time.Since(start) - detectDur - extractDur

	inputTokens := det.TotalWords * 100 / 75
	res := &Result{
		Root: opts.Root, OutDir: opts.OutDir, Graph: g, Communities: comms, Labels: labels, Cohesion: cohesion,
		Gods: gods, Surprises: surprises, Questions: questions, Detection: det,
		FilesExtract: int(extracted), FilesCached: int(cached), FilesFailed: int(failed),
		InputTokens: inputTokens, OutputTokens: 0,
		DetectTime: detectDur, ExtractTime: extractDur, ClusterTime: clusterDur, Mentions: mentions,
		Concepts: concepts, ConceptMentions: conceptMentions, Semantic: semanticEdges,
	}
	if semanticWarn != "" {
		res.Warnings = append(res.Warnings, semanticWarn)
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, err
	}
	f := opts.Formats
	if f.JSON {
		if err := export.WriteJSON(filepath.Join(opts.OutDir, "graph.json"), g); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "graph.json")
	}
	if f.Report {
		d := report.Data{
			Root: reportRoot(opts), Graph: g, Communities: comms, Cohesion: cohesion,
			Labels: labels, GodNodes: gods, Surprises: surprises, Questions: questions,
			Detection: det, Mode: opts.Mode, InputTokens: inputTokens, OutputTokens: 0,
			Mentions: mentions,
		}
		if err := report.Write(filepath.Join(opts.OutDir, "GRAPH_REPORT.md"), d); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "GRAPH_REPORT.md")
	}
	if f.HTML {
		err := export.WriteHTML(filepath.Join(opts.OutDir, "graph.html"), g, comms, labels, cohesion, opts.Root)
		switch {
		case err == nil:
			res.Outputs = append(res.Outputs, "graph.html")
		case errors.Is(err, export.ErrTooLarge):
			// The viewer has a hard node budget; skip it rather than failing a
			// build that is otherwise complete.
			res.Warnings = append(res.Warnings, err.Error())
			if opts.Logf != nil {
				opts.Logf("skipping graph.html: %v", err)
			}
		default:
			return nil, err
		}
	}
	if f.Obsidian {
		if len(g.Nodes) > MaxObsidianNodes && !f.ObsidianForced {
			msg := fmt.Sprintf("graph has %d nodes - skipping obsidian/ (pass --obsidian to force)", len(g.Nodes))
			res.Warnings = append(res.Warnings, msg)
			if opts.Logf != nil {
				opts.Logf("%s", msg)
			}
		} else if _, err := export.WriteObsidian(filepath.Join(opts.OutDir, "obsidian"), g, comms, labels); err != nil {
			return nil, err
		} else {
			if err := export.WriteObsidianIndex(filepath.Join(opts.OutDir, "obsidian"), g, labels, comms); err != nil {
				return nil, err
			}
			res.Outputs = append(res.Outputs, "obsidian/")
		}
	}
	if f.Wiki {
		if err := export.WriteWiki(filepath.Join(opts.OutDir, "wiki"), g, comms, labels, cohesion, gods); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "wiki/")
	}
	if f.SVG {
		if err := export.WriteSVG(filepath.Join(opts.OutDir, "graph.svg"), g, comms, labels); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "graph.svg")
	}
	if f.GraphML {
		if err := export.WriteGraphML(filepath.Join(opts.OutDir, "graph.graphml"), g, comms, labels); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "graph.graphml")
	}
	if f.Cypher {
		if err := export.WriteCypher(filepath.Join(opts.OutDir, "graph.cypher"), g); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "graph.cypher")
	}
	if f.Canvas {
		if err := export.WriteCanvas(filepath.Join(opts.OutDir, "graph.canvas"), g, comms, labels); err != nil {
			return nil, err
		}
		res.Outputs = append(res.Outputs, "graph.canvas")
	}
	if opts.Neo4j != nil {
		n, err := export.PushNeo4j(g, *opts.Neo4j)
		if err != nil {
			return nil, fmt.Errorf("neo4j push: %w", err)
		}
		res.Neo4jSent = n
	}

	// Manifest for the next incremental run.
	m := &cache.Manifest{Files: make(map[string]cache.FileEntry, len(jobs))}
	for i := range jobs {
		fi, err := os.Stat(jobs[i].path)
		if err != nil {
			continue
		}
		digest, err := cache.HashFile(jobs[i].path)
		if err != nil {
			continue
		}
		m.Files[jobs[i].rel] = cache.FileEntry{Digest: digest, Size: fi.Size(), ModTime: fi.ModTime().UnixNano(), Kind: string(jobs[i].kind)}
	}
	m.Root = opts.Root
	m.Tool = toolTag
	m.Nodes = len(g.Nodes)
	m.Edges = len(g.Edges)
	m.Communities = len(comms)
	m.TotalWords = det.TotalWords
	if err := cache.SaveManifest(opts.Root, m); err != nil {
		return nil, err
	}
	res.Elapsed = time.Since(start)
	res.WriteTime = res.Elapsed - detectDur - extractDur - clusterDur
	return res, nil
}

func reportRoot(opts Options) string {
	if opts.Label != "" {
		return opts.Label
	}
	if opts.Root == "." || opts.Root == "" {
		if wd, err := os.Getwd(); err == nil {
			return filepath.Base(wd)
		}
	}
	return opts.Root
}

func displayRel(root, path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.ToSlash(filepath.Clean(root))
	if root == "." || root == "" {
		return clean
	}
	if rel, err := filepath.Rel(base, clean); err == nil && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel)
	}
	return clean
}
