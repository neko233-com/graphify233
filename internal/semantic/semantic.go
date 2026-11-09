// Package semantic adds optional, opt-in similarity edges to a graph.
//
// Two layers are available:
//   - Concepts: deterministic Jaccard similarity between documentation concepts
//     based on the code symbols each section mentions.
//   - Embeddings: an OpenAI-compatible embeddings endpoint, used only when the
//     caller explicitly passes a URL. The core build never calls an LLM.
package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/neko233-com/graphify233/internal/model"
)

// Limits bounds the number of inferred edges.
type Limits struct {
	MaxEdges   int
	MaxPerNode int
}

func (l Limits) withDefaults() Limits {
	if l.MaxEdges <= 0 {
		l.MaxEdges = 2000
	}
	if l.MaxPerNode <= 0 {
		l.MaxPerNode = 8
	}
	return l
}

// Concepts links concept nodes whose sections mention overlapping symbol sets.
// The similarity is the Jaccard index over mentioned symbols, so it is fully
// deterministic and needs no model.
func Concepts(g *model.Graph, minJaccard float64, lim Limits) int {
	lim = lim.withDefaults()
	if minJaccard <= 0 || minJaccard > 1 {
		minJaccard = 0.5
	}
	sets := map[int32]map[int32]bool{}
	for _, e := range g.Edges {
		if e.Relation != "mentions" {
			continue
		}
		s := g.Index(e.Source)
		t := g.Index(e.Target)
		if s < 0 || t < 0 {
			continue
		}
		src := g.Nodes[s]
		if src.FileType != model.FileTypeDocument {
			continue
		}
		if sets[s] == nil {
			sets[s] = map[int32]bool{}
		}
		sets[s][t] = true
	}
	ids := make([]int32, 0, len(sets))
	for id, set := range sets {
		if len(set) >= 1 {
			ids = append(ids, id)
		}
	}
	// Rare shared symbols matter more than ubiquitous ones: weight the
	// Jaccard index by inverse document frequency.
	df := map[int32]int{}
	for _, set := range sets {
		for id := range set {
			df[id]++
		}
	}
	idf := func(id int32) float64 {
		return math.Log(1 + float64(len(sets))/float64(df[id]))
	}
	sort.Slice(ids, func(i, j int) bool { return g.Nodes[ids[i]].ID < g.Nodes[ids[j]].ID })

	added := 0
	perNode := map[int32]int{}
	for i := 0; i < len(ids) && added < lim.MaxEdges; i++ {
		for j := i + 1; j < len(ids) && added < lim.MaxEdges; j++ {
			a, b := ids[i], ids[j]
			if perNode[a] >= lim.MaxPerNode && perNode[b] >= lim.MaxPerNode {
				continue
			}
			shared, wa, wb := 0.0, 0.0, 0.0
			for id := range sets[a] {
				wa += idf(id)
				if sets[b][id] {
					shared += idf(id)
				}
			}
			for id := range sets[b] {
				wb += idf(id)
			}
			if shared == 0 || wa == 0 || wb == 0 {
				continue
			}
			// IDF-weighted overlap coefficient: how much of the smaller
			// section is covered by the other one.
			jac := shared / math.Min(wa, wb)
			if jac < minJaccard {
				continue
			}
			if g.HasEdge(g.Nodes[a].ID, g.Nodes[b].ID) {
				continue
			}
			if !g.AddEdge(model.Edge{
				Source:          g.Nodes[a].ID,
				Target:          g.Nodes[b].ID,
				Relation:        "semantically_similar_to",
				Confidence:      model.ConfInferred,
				Weight:          math.Round(jac*100) / 100,
				ConfidenceScore: math.Round(jac*100) / 100,
			}) {
				continue
			}
			added++
			perNode[a]++
			perNode[b]++
		}
	}
	return added
}

// Embedder turns texts into vectors. EmbeddingClient uses a remote
// OpenAI-compatible service; HashingEmbedder runs locally with no network.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// EmbeddingClient talks to an OpenAI-compatible /embeddings endpoint.
type EmbeddingClient struct {
	URL    string
	Model  string
	APIKey string
	Client *http.Client
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed returns one vector per input text.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("no embeddings URL configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out embedResponse
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 32<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		msg := resp.Status
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return nil, fmt.Errorf("embeddings endpoint: %s", msg)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings endpoint returned %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float64, len(out.Data))
	for i := range out.Data {
		vecs[i] = out.Data[i].Embedding
	}
	return vecs, nil
}

// HashingEmbedder is a deterministic, offline embedder. Texts are split into
// lowercase alphanumeric tokens, each token is hashed into one of Dim buckets
// (FNV-1a) with a sign bit, weighted by inverse document frequency so common
// tokens such as the project name or "readme" do not dominate, and the vector
// is L2-normalised. Labels sharing informative tokens end up close, so related
// identifiers connect without a network call, model, or API key.
type HashingEmbedder struct {
	Dim int
}

// Embed implements Embedder.
func (h HashingEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	dim := h.Dim
	if dim <= 0 {
		dim = 512
	}
	tokenDocs := make([][]string, len(texts))
	df := make(map[string]int)
	for i, text := range texts {
		toks := tokenize(text)
		tokenDocs[i] = toks
		seen := make(map[string]bool, len(toks))
		for _, tok := range toks {
			if !seen[tok] {
				seen[tok] = true
				df[tok]++
			}
		}
	}
	out := make([][]float64, len(texts))
	n := float64(len(texts))
	for i, toks := range tokenDocs {
		vec := make([]float64, dim)
		for _, tok := range toks {
			sum := fnv32a(tok)
			w := math.Log(1 + n/float64(df[tok]))
			idx := int(sum % uint32(dim))
			if sum&(1<<31) != 0 {
				vec[idx] -= w
			} else {
				vec[idx] += w
			}
		}
		var norm float64
		for _, x := range vec {
			norm += x * x
		}
		if norm > 0 {
			norm = math.Sqrt(norm)
			for j := range vec {
				vec[j] /= norm
			}
		}
		out[i] = vec
	}
	return out, nil
}

func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := fields[:0]
	for _, tok := range fields {
		if len(tok) >= 2 {
			out = append(out, tok)
		}
	}
	return out
}

func fnv32a(s string) uint32 {
	const offset, prime uint32 = 2166136261, 16777619
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

// EmbeddingLinks embeds symbol labels and connects each symbol to its most
// similar neighbours above threshold. This is the explicit opt-in semantic
// layer: it only runs when the caller configures an embedder.
func EmbeddingLinks(ctx context.Context, g *model.Graph, client Embedder, threshold float64, lim Limits) (int, error) {
	lim = lim.withDefaults()
	if threshold <= 0 || threshold > 1 {
		threshold = 0.82
	}
	if client == nil {
		return 0, nil
	}
	var ids []int32
	texts := make([]string, 0, 256)
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.FileType != model.FileTypeCode || n.SourceFile == "" {
			continue
		}
		if model.IsFileExtLabel(n.Label) || strings.TrimSpace(n.Label) == "" {
			continue
		}
		if len(texts) >= 2000 {
			break
		}
		ids = append(ids, int32(i))
		texts = append(texts, strings.TrimSuffix(n.Label, "()"))
	}
	if len(texts) < 2 {
		return 0, nil
	}
	vecs, err := client.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	// The hashing embedder is a bag-of-tokens model: disjoint token sets mean
	// unrelated texts, and rare hash collisions of distinct single-token
	// labels (main vs graphmlDoc) must not fabricate neighbours. Gate
	// candidates on an actual shared token; external embedders are left
	// ungated because they are supposed to generalise beyond exact tokens.
	var gate func(i, j int) bool
	if _, ok := client.(HashingEmbedder); ok {
		sets := make([]map[string]bool, len(texts))
		for i, text := range texts {
			s := make(map[string]bool, 4)
			for _, tok := range tokenize(text) {
				s[tok] = true
			}
			sets[i] = s
		}
		gate = func(i, j int) bool {
			for tok := range sets[i] {
				if sets[j][tok] {
					return true
				}
			}
			return false
		}
	}
	norm := make([]float64, len(vecs))
	for i, v := range vecs {
		var s float64
		for _, x := range v {
			s += x * x
		}
		norm[i] = math.Sqrt(s)
	}
	added := 0
	perNode := make([]int, len(ids))
	for i := range ids {
		if norm[i] == 0 {
			continue
		}
		type cand struct {
			j   int
			cos float64
		}
		var cands []cand
		for j := range ids {
			if i == j || norm[j] == 0 || perNode[i] >= lim.MaxPerNode {
				continue
			}
			if g.HasEdge(g.Nodes[ids[i]].ID, g.Nodes[ids[j]].ID) {
				continue
			}
			// Identically named symbols in different scopes (main, init,
			// String) are not semantic neighbours; linking every pair would
			// bury the useful edges.
			if strings.EqualFold(texts[i], texts[j]) {
				continue
			}
			if gate != nil && !gate(i, j) {
				continue
			}
			cos := cosine(vecs[i], vecs[j], norm[i], norm[j])
			if cos >= threshold {
				cands = append(cands, cand{j: j, cos: cos})
			}
		}
		sort.Slice(cands, func(a, b int) bool { return cands[a].cos > cands[b].cos })
		for _, c := range cands {
			if added >= lim.MaxEdges || perNode[i] >= lim.MaxPerNode {
				break
			}
			if g.HasEdge(g.Nodes[ids[i]].ID, g.Nodes[ids[c.j]].ID) {
				continue
			}
			if !g.AddEdge(model.Edge{
				Source:          g.Nodes[ids[i]].ID,
				Target:          g.Nodes[ids[c.j]].ID,
				Relation:        "semantically_similar_to",
				Confidence:      model.ConfInferred,
				Weight:          math.Round(c.cos*100) / 100,
				ConfidenceScore: math.Round(c.cos*100) / 100,
			}) {
				continue
			}
			added++
			perNode[i]++
			perNode[c.j]++
		}
	}
	return added, nil
}

func cosine(a, b []float64, na, nb float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot / (na * nb)
}
