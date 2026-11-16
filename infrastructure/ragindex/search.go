package ragindex

import "container/heap"

type candidate struct {
	id   int32
	dist float32
}

type minCandidates []candidate

func (h minCandidates) Len() int           { return len(h) }
func (h minCandidates) Less(i, j int) bool { return h[i].dist < h[j].dist }
func (h minCandidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minCandidates) Push(x any)        { *h = append(*h, x.(candidate)) }
func (h *minCandidates) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

type maxCandidates []candidate

func (h maxCandidates) Len() int           { return len(h) }
func (h maxCandidates) Less(i, j int) bool { return h[i].dist > h[j].dist }
func (h maxCandidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxCandidates) Push(x any)        { *h = append(*h, x.(candidate)) }
func (h *maxCandidates) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// greedy walks one layer toward the query until no neighbor is closer.
func (g *graph) greedy(entry int32, query []float32, layer int) int32 {
	current := entry
	currentDist := distance(query, g.nodes[current].vector)
	for {
		next, dist := g.closestNeighbor(current, query, layer, currentDist)
		if next == current {
			return current
		}
		current, currentDist = next, dist
	}
}

func (g *graph) closestNeighbor(current int32, query []float32, layer int, best float32) (int32, float32) {
	nearest := current
	if layer >= len(g.nodes[current].links) {
		return nearest, best
	}
	for _, neighbor := range g.nodes[current].links[layer] {
		dist := distance(query, g.nodes[neighbor].vector)
		if dist < best {
			nearest, best = neighbor, dist
		}
	}
	return nearest, best
}

// searchLayer is the standard HNSW beam search: a candidate min-heap explores
// the graph, a result max-heap keeps the best ef nodes, and the result is
// returned closest-first.
func (g *graph) searchLayer(query []float32, entries []int32, ef, layer int) []candidate {
	if ef < 1 {
		ef = 1
	}
	visited := make(map[int32]struct{}, ef*4)
	candidates := &minCandidates{}
	results := &maxCandidates{}
	g.seedLayer(query, entries, layer, visited, candidates, results)
	for !shouldStop(candidates, results, ef) {
		current := heap.Pop(candidates).(candidate)
		g.visitNeighbors(query, layer, ef, current.id, visited, candidates, results)
	}
	out := make([]candidate, len(*results))
	copy(out, *results)
	sortCandidates(out)
	return out
}

func (g *graph) seedLayer(
	query []float32,
	entries []int32,
	layer int,
	visited map[int32]struct{},
	candidates *minCandidates,
	results *maxCandidates,
) {
	for _, id := range entries {
		if int(id) < 0 || int(id) >= len(g.nodes) || layer >= len(g.nodes[id].links) {
			continue
		}
		if _, seen := visited[id]; seen {
			continue
		}
		visited[id] = struct{}{}
		dist := distance(query, g.nodes[id].vector)
		heap.Push(candidates, candidate{id: id, dist: dist})
		heap.Push(results, candidate{id: id, dist: dist})
	}
}

func shouldStop(candidates *minCandidates, results *maxCandidates, ef int) bool {
	if candidates.Len() == 0 {
		return true
	}
	return results.Len() >= ef && (*candidates)[0].dist > (*results)[0].dist
}

func (g *graph) visitNeighbors(
	query []float32,
	layer, ef int,
	current int32,
	visited map[int32]struct{},
	candidates *minCandidates,
	results *maxCandidates,
) {
	for _, neighbor := range g.nodes[current].links[layer] {
		g.considerNeighbor(query, ef, neighbor, visited, candidates, results)
	}
}

func (g *graph) considerNeighbor(
	query []float32,
	ef int,
	neighbor int32,
	visited map[int32]struct{},
	candidates *minCandidates,
	results *maxCandidates,
) {
	if _, seen := visited[neighbor]; seen {
		return
	}
	visited[neighbor] = struct{}{}
	dist := distance(query, g.nodes[neighbor].vector)
	if results.Len() >= ef && dist >= (*results)[0].dist {
		return
	}
	heap.Push(candidates, candidate{id: neighbor, dist: dist})
	heap.Push(results, candidate{id: neighbor, dist: dist})
	if results.Len() > ef {
		heap.Pop(results)
	}
}

func sortCandidates(items []candidate) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].dist < items[j-1].dist; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// search returns the k closest node indexes for a normalized query. A zero
// vector yields no graph result; the caller then uses the exact scan.
func (g *graph) search(query []float32, k, ef int) []int32 {
	normalized := normalize(query)
	if normalized == nil || g.entry < 0 || k <= 0 {
		return nil
	}
	if ef < k {
		ef = k
	}
	entry := g.entry
	for l := g.maxLevel; l > 0; l-- {
		entry = g.greedy(entry, normalized, l)
	}
	found := g.searchLayer(normalized, []int32{entry}, ef, 0)
	out := make([]int32, 0, k)
	for _, item := range found {
		if len(out) == k {
			break
		}
		out = append(out, item.id)
	}
	return out
}
