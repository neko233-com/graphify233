// Package benchmark estimates how many tokens a graph query needs compared
// with feeding the whole corpus to a model.
package benchmark

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/neko233-com/graphify233/internal/model"
)

// CharsPerToken is the usual rough English/code ratio.
const CharsPerToken = 4

// DefaultQuestions mirrors the reference benchmark corpus checks.
var DefaultQuestions = []string{
	"how does authentication work",
	"what is the main entry point",
	"how are errors handled",
	"what connects the data layer to the api",
	"what are the core abstractions",
}

// Question is one measured query.
type Question struct {
	Question    string  `json:"question"`
	QueryTokens int     `json:"query_tokens"`
	Reduction   float64 `json:"reduction_ratio"`
}

// Report is the benchmark result.
type Report struct {
	CorpusTokens   int        `json:"corpus_tokens"`
	CorpusWords    int        `json:"corpus_words"`
	Nodes          int        `json:"nodes"`
	Edges          int        `json:"edges"`
	AvgQueryTokens int        `json:"avg_query_tokens"`
	ReductionRatio float64    `json:"reduction_ratio"`
	Questions      []Question `json:"per_question"`
}

// EstimateTokens approximates a token count from a character count.
func EstimateTokens(chars int) int {
	if chars <= 0 {
		return 1
	}
	return chars / CharsPerToken
}

// QuerySubgraphTokens builds the same term-scored, depth-3 BFS text the query
// engine produces and returns its estimated token count.
func QuerySubgraphTokens(g *model.Graph, question string) int {
	terms := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	var usable []string
	for _, t := range terms {
		if len(t) > 2 {
			usable = append(usable, t)
		}
	}
	type scored struct {
		idx   int32
		score int
	}
	var hits []scored
	for i := range g.Nodes {
		label := strings.ToLower(g.Nodes[i].Label)
		score := 0
		for _, t := range usable {
			if strings.Contains(label, t) {
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
		return g.Degree(hits[i].idx) > g.Degree(hits[j].idx)
	})
	if len(hits) == 0 {
		return 0 // question matched nothing: excluded from the average
	}
	if len(hits) > 3 {
		hits = hits[:3]
	}
	seen := make([]bool, len(g.Nodes))
	var nodes []int32
	frontier := make([]int32, 0, len(hits))
	for _, h := range hits {
		if !seen[h.idx] {
			seen[h.idx] = true
			nodes = append(nodes, h.idx)
			frontier = append(frontier, h.idx)
		}
	}
	for d := 0; d < 3 && len(frontier) > 0; d++ {
		var next []int32
		for _, u := range frontier {
			for _, v := range g.Adj()[u] {
				if !seen[v] {
					seen[v] = true
					nodes = append(nodes, v)
					next = append(next, v)
				}
			}
		}
		frontier = next
	}
	var b strings.Builder
	for _, n := range nodes {
		node := g.Nodes[n]
		src := node.SourceFile
		if src == "" {
			src = "external"
		}
		fmt.Fprintf(&b, "NODE %s src=%s loc=%s\n", node.Label, src, node.SourceLocation)
	}
	for _, e := range g.Edges {
		s, t := g.Index(e.Source), g.Index(e.Target)
		if s < 0 || t < 0 || !seen[s] || !seen[t] {
			continue
		}
		fmt.Fprintf(&b, "EDGE %s --%s--> %s\n", g.Nodes[s].Label, e.Relation, g.Nodes[t].Label)
	}
	return EstimateTokens(b.Len())
}

// Run measures every question against a corpus word count.
func Run(g *model.Graph, corpusWords int, questions []string) Report {
	if len(questions) == 0 {
		questions = DefaultQuestions
	}
	r := Report{
		CorpusWords:  corpusWords,
		CorpusTokens: corpusWords * 100 / 75,
		Nodes:        len(g.Nodes),
		Edges:        len(g.Edges),
		Questions:    make([]Question, 0, len(questions)),
	}
	total, matched := 0, 0
	for _, q := range questions {
		tokens := QuerySubgraphTokens(g, q)
		red := 0.0
		if tokens > 0 {
			total += tokens
			matched++
			red = float64(r.CorpusTokens) / float64(tokens)
		}
		r.Questions = append(r.Questions, Question{Question: q, QueryTokens: tokens, Reduction: red})
	}
	if matched > 0 {
		r.AvgQueryTokens = total / matched
		r.ReductionRatio = float64(r.CorpusTokens) / float64(r.AvgQueryTokens)
	}
	return r
}

// Print renders the benchmark report.
func Print(w io.Writer, r Report) {
	line := strings.Repeat("─", 62)
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "  Graph Benchmark · %d nodes · %d edges\n", r.Nodes, r.Edges)
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "  Corpus:        ~%s tokens (%s words)\n", commas(r.CorpusTokens), commas(r.CorpusWords))
	fmt.Fprintf(w, "  Avg query:     ~%s tokens\n", commas(r.AvgQueryTokens))
	if r.ReductionRatio >= 1.5 {
		fmt.Fprintf(w, "  Token savings: %.1fx fewer tokens per query\n", r.ReductionRatio)
	} else {
		fmt.Fprintf(w, "  Token savings: %.2fx (corpus barely larger than a query)\n", r.ReductionRatio)
	}
	fmt.Fprintln(w, line)
	for _, q := range r.Questions {
		if q.QueryTokens == 0 {
			fmt.Fprintf(w, "  %-52s %9s\n", truncate(q.Question, 52), "no match")
			continue
		}
		fmt.Fprintf(w, "  %-52s ~%6s  %5.1fx\n", truncate(q.Question, 52), commas(q.QueryTokens), q.Reduction)
	}
	fmt.Fprintln(w, line)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func commas(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
