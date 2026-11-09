// Package cluster detects communities with a deterministic Louvain
// implementation (local moving + modularity aggregation). Communities larger
// than a fraction of the graph are recursively re-clustered, mirroring the
// reference tool's Leiden size cap.
package cluster

import "sort"

// Size-cap parameters mirror the Python reference.
const (
	MaxCommunityFraction = 0.25
	MinSplitSize         = 10
	MaxSplitDepth        = 4
)

// weighted adjacency used at each Louvain level.
type level struct {
	n    int
	nbr  [][]int32
	w    [][]float64
	deg  []float64
	self []float64 // internal weight of an aggregated node (self-loop)
}

// Detect clusters the graph and returns communities ordered by size (largest
// first). Members are dense node indices, sorted ascending.
func Detect(n int, adj [][]int32) [][]int32 {
	if n == 0 {
		return nil
	}
	isolated := make([]int32, 0, 8)
	active := make([]int32, 0, n)
	for i := 0; i < n; i++ {
		if len(adj[i]) == 0 {
			isolated = append(isolated, int32(i))
			continue
		}
		active = append(active, int32(i))
	}

	var comms [][]int32
	if len(active) > 0 {
		comms = louvain(active, adj)
	}
	for _, i := range isolated {
		comms = append(comms, []int32{i})
	}
	if len(comms) == 0 {
		return nil
	}

	maxSize := int(float64(n) * MaxCommunityFraction)
	if maxSize < MinSplitSize {
		maxSize = MinSplitSize
	}
	if len(comms) > 1 || len(comms[0]) > maxSize {
		var capped [][]int32
		for _, c := range comms {
			capped = append(capped, splitOversized(c, adj, maxSize, 0)...)
		}
		comms = capped
	}
	sortCommunities(comms)
	return comms
}

func sortCommunities(comms [][]int32) {
	for _, c := range comms {
		sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	}
	sort.SliceStable(comms, func(i, j int) bool {
		if len(comms[i]) != len(comms[j]) {
			return len(comms[i]) > len(comms[j])
		}
		return comms[i][0] < comms[j][0]
	})
}

// StableOrder renumbers communities so that their indices survive re-runs.
// Detect orders communities by size, so a single added file shifts every id
// and makes a graph diff look like massive community drift. Here each
// community is anchored to its highest-degree member (ties broken by the
// smaller node id) and communities are ordered by anchor id; unchanged
// communities keep their index, and a node's movement is actually visible in
// "community moved" output. Members keep their ascending index order.
func StableOrder(comms [][]int32, ids []string, deg []int32) [][]int32 {
	if len(comms) < 2 {
		return comms
	}
	type entry struct {
		comm   []int32
		anchor string
	}
	entries := make([]entry, 0, len(comms))
	for _, c := range comms {
		best := c[0]
		for _, m := range c[1:] {
			if deg[m] > deg[best] || (deg[m] == deg[best] && ids[m] < ids[best]) {
				best = m
			}
		}
		entries = append(entries, entry{comm: c, anchor: ids[best]})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].anchor < entries[j].anchor })
	out := make([][]int32, len(entries))
	for i := range entries {
		out[i] = entries[i].comm
	}
	return out
}

// splitOversized re-clusters a too-large community on its induced subgraph.
func splitOversized(members []int32, adj [][]int32, maxSize, depth int) [][]int32 {
	if len(members) <= maxSize || depth >= MaxSplitDepth {
		return [][]int32{members}
	}
	sub := induced(members, adj)
	parts := louvainOn(sub)
	if len(parts) <= 1 {
		return [][]int32{members}
	}
	var out [][]int32
	for _, p := range parts {
		group := make([]int32, len(p))
		for i, li := range p {
			group[i] = members[li]
		}
		if len(group) > maxSize {
			out = append(out, splitOversized(group, adj, maxSize, depth+1)...)
		} else {
			out = append(out, group)
		}
	}
	return out
}

// induced builds the adjacency of the subgraph over members, with local ids.
func induced(members []int32, adj [][]int32) [][]int32 {
	m := len(members)
	local := make(map[int32]int32, m)
	for i, v := range members {
		local[v] = int32(i)
	}
	sub := make([][]int32, m)
	seen := make(map[uint64]struct{}, m*2)
	for i, v := range members {
		for _, nb := range adj[v] {
			li, ok := local[nb]
			if !ok {
				continue
			}
			lo, hi := int32(i), li
			if lo > hi {
				lo, hi = hi, lo
			}
			key := uint64(uint32(lo))<<32 | uint64(uint32(hi))
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			sub[i] = append(sub[i], li)
		}
	}
	return sub
}

// louvainOn clusters a plain unweighted adjacency (local ids).
func louvainOn(sub [][]int32) [][]int32 {
	m := len(sub)
	if m == 0 {
		return nil
	}
	cur := &level{n: m, nbr: sub, w: make([][]float64, m), deg: make([]float64, m)}
	for i := 0; i < m; i++ {
		cur.w[i] = make([]float64, len(sub[i]))
		for j := range cur.w[i] {
			cur.w[i][j] = 1
		}
		cur.deg[i] = float64(len(sub[i]))
	}
	members := make([][]int32, m)
	for i := range members {
		members[i] = []int32{int32(i)}
	}
	groups := runLevels(cur, members)
	out := make([][]int32, len(groups))
	for i, g := range groups {
		out[i] = g
	}
	return out
}

// louvain clusters the active subgraph and maps results back to global ids.
func louvain(active []int32, adj [][]int32) [][]int32 {
	m := len(active)
	local := make(map[int32]int32, m)
	for i, v := range active {
		local[v] = int32(i)
	}
	cur := &level{n: m, nbr: make([][]int32, m), w: make([][]float64, m), deg: make([]float64, m)}
	for i, v := range active {
		neigh := adj[v]
		cur.nbr[i] = make([]int32, 0, len(neigh))
		cur.w[i] = make([]float64, 0, len(neigh))
		for _, nb := range neigh {
			li, ok := local[nb]
			if !ok {
				continue
			}
			cur.nbr[i] = append(cur.nbr[i], li)
			cur.w[i] = append(cur.w[i], 1)
		}
		cur.deg[i] = float64(len(cur.nbr[i]))
	}
	members := make([][]int32, m)
	for i, v := range active {
		members[i] = []int32{v}
	}
	return runLevels(cur, members)
}

// runLevels performs local moving and aggregation until communities stabilize.
func runLevels(cur *level, members [][]int32) [][]int32 {
	for depth := 0; depth < 16; depth++ {
		comm := localMove(cur)
		k := 0
		remap := make([]int32, cur.n)
		for i := range remap {
			remap[i] = -1
		}
		for i := 0; i < cur.n; i++ {
			c := comm[i]
			if remap[c] < 0 {
				remap[c] = int32(k)
				k++
			}
		}
		if k == cur.n {
			// Nothing merged at this level: the partition is final.
			return members
		}
		newMembers := make([][]int32, k)
		for i := 0; i < cur.n; i++ {
			j := remap[comm[i]]
			newMembers[j] = append(newMembers[j], members[i]...)
		}
		cur = aggregate(cur, comm, remap, k)
		members = newMembers
	}
	// Depth guard: treat current partition as final.
	return members
}

func aggregate(cur *level, comm []int32, remap []int32, k int) *level {
	next := &level{n: k, nbr: make([][]int32, k), w: make([][]float64, k), deg: make([]float64, k), self: make([]float64, k)}
	type pair struct{ a, b int32 }
	agg := make(map[uint64]float64, cur.n)
	for i := 0; i < cur.n; i++ {
		a := remap[comm[i]]
		if cur.self != nil {
			next.self[a] += cur.self[i]
		}
		for j, nb := range cur.nbr[i] {
			b := remap[comm[nb]]
			if a == b {
				// Internal edge becomes a self-loop, counted once per edge.
				next.self[a] += cur.w[i][j] / 2
				continue
			}
			if a > b {
				// Each undirected edge appears twice (once from each side);
				// keep only the lower-id direction so the aggregated weight
				// equals the original edge weight instead of double-counting.
				continue
			}
			agg[uint64(uint32(a))<<32|uint64(uint32(b))] += cur.w[i][j]
		}
	}
	for key, w := range agg {
		a := int32(key >> 32)
		b := int32(uint32(key))
		next.nbr[a] = append(next.nbr[a], b)
		next.w[a] = append(next.w[a], w)
		next.nbr[b] = append(next.nbr[b], a)
		next.w[b] = append(next.w[b], w)
		next.deg[a] += w
		next.deg[b] += w
	}
	// Self-loops carry internal cohesion: a node's degree must include them so
	// that merging two dense communities stops looking profitable.
	for i := 0; i < k; i++ {
		next.deg[i] += 2 * next.self[i]
	}
	// Deterministic neighbour order for reproducible tie-breaking.
	for i := 0; i < k; i++ {
		order := make([]int, len(next.nbr[i]))
		for j := range order {
			order[j] = j
		}
		sort.Slice(order, func(x, y int) bool { return next.nbr[i][order[x]] < next.nbr[i][order[y]] })
		nbr := make([]int32, len(order))
		wt := make([]float64, len(order))
		for j, o := range order {
			nbr[j] = next.nbr[i][o]
			wt[j] = next.w[i][o]
		}
		next.nbr[i] = nbr
		next.w[i] = wt
	}
	return next
}

// localMove optimizes modularity by moving single nodes between communities.
func localMove(g *level) []int32 {
	comm := make([]int32, g.n)
	tot := make([]float64, g.n)
	m2 := 0.0
	for i := 0; i < g.n; i++ {
		comm[i] = int32(i)
		tot[i] = g.deg[i]
		m2 += g.deg[i]
	}
	if m2 == 0 {
		return comm
	}
	wsum := make([]float64, g.n)
	stamp := make([]int32, g.n)
	curStamp := int32(0)
	cand := make([]int32, 0, 16)

	for pass := 0; pass < 24; pass++ {
		moved := false
		for i := 0; i < g.n; i++ {
			if len(g.nbr[i]) == 0 {
				continue
			}
			curStamp++
			cand = cand[:0]
			for j, nb := range g.nbr[i] {
				c := comm[nb]
				if stamp[c] != curStamp {
					stamp[c] = curStamp
					wsum[c] = 0
					cand = append(cand, c)
				}
				wsum[c] += g.w[i][j]
			}
			ci := comm[i]
			ki := g.deg[i]
			// Keep the "stay" weight correct even when no neighbour belongs to
			// the node's current community (stamp prevents stale values).
			if stamp[ci] != curStamp {
				stamp[ci] = curStamp
				wsum[ci] = 0
			}
			tot[ci] -= ki
			best := ci
			bestGain := wsum[ci] - tot[ci]*ki/m2
			for _, c := range cand {
				gain := wsum[c] - tot[c]*ki/m2
				// Strict improvement keeps the current community on ties, which
				// avoids every node draining into the lowest-numbered cluster.
				if gain > bestGain+1e-12 {
					best = c
					bestGain = gain
				}
			}
			tot[best] += ki
			if best != ci {
				comm[i] = best
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return comm
}

// Cohesion returns the internal edge density of a community (0..1).
func Cohesion(members []int32, adj [][]int32, stamp []int32, mark int32) float64 {
	n := len(members)
	if n < 2 {
		return 0
	}
	for _, m := range members {
		stamp[m] = mark
	}
	edges := 0
	for _, m := range members {
		for _, nb := range adj[m] {
			if stamp[nb] == mark {
				edges++
			}
		}
	}
	edges /= 2
	possible := n * (n - 1) / 2
	if possible == 0 {
		return 0
	}
	return float64(edges) / float64(possible)
}
