package ragindex

import (
	"sort"

	domainrag "bridge-gateway/domain/rag"
)

// insert adds one chunk to the graph, or replaces its vector/text when the same
// chunk ID is inserted again (document re-ingestion).
func (g *graph) insert(chunk domainrag.Chunk) bool {
	vec := normalize(chunk.Vector)
	if vec == nil {
		return false
	}
	if id, ok := g.byID[chunk.ID]; ok {
		g.nodes[id].chunk = chunk
		g.nodes[id].vector = vec
		return true
	}

	level := g.randomLevel()
	idx := int32(len(g.nodes))
	g.nodes = append(g.nodes, node{
		chunk:  chunk,
		vector: vec,
		level:  level,
		links:  make([][]int32, level+1),
	})
	g.byID[chunk.ID] = idx
	if g.entry < 0 {
		g.entry, g.maxLevel = idx, level
		return true
	}

	entry := g.entry
	for l := g.maxLevel; l > level; l-- {
		entry = g.greedy(entry, vec, l)
	}
	g.connect(idx, entry, vec, level)
	if level > g.maxLevel {
		g.maxLevel, g.entry = level, idx
	}
	return true
}

// connect links a new node at every layer it belongs to, using efConstruction
// candidates per layer and bidirectional edges.
func (g *graph) connect(idx int32, entry int32, vec []float32, level int) {
	top := level
	if g.maxLevel < top {
		top = g.maxLevel
	}
	for l := top; l >= 0; l-- {
		found := g.searchLayer(vec, []int32{entry}, g.efConstruction, l)
		selected := nearest(found, g.m)
		g.nodes[idx].links[l] = selected
		for _, neighbor := range selected {
			g.link(int(neighbor), int(idx), l)
		}
		if len(found) > 0 {
			entry = found[0].id
		}
	}
}

// nearest keeps the closest candidates, preserving their distance order.
func nearest(found []candidate, limit int) []int32 {
	if limit < 1 {
		return nil
	}
	if len(found) > limit {
		found = found[:limit]
	}
	out := make([]int32, len(found))
	for i, item := range found {
		out[i] = item.id
	}
	return out
}

// link adds a bidirectional edge. Neighbor lists are pruned to the HNSW caps:
// 2*M at layer 0 and M above.
func (g *graph) link(from, to, layer int) {
	if from == to || layer < 0 || layer >= len(g.nodes[from].links) || layer >= len(g.nodes[to].links) {
		return
	}
	for _, existing := range g.nodes[from].links[layer] {
		if existing == int32(to) {
			return
		}
	}
	g.nodes[from].links[layer] = append(g.nodes[from].links[layer], int32(to))
	cap := g.m
	if layer == 0 {
		cap = g.maxLinks
	}
	if len(g.nodes[from].links[layer]) > cap {
		g.prune(from, layer, cap)
	}
}

// prune keeps the cap closest neighbors of a node at one layer.
func (g *graph) prune(from, layer, limit int) {
	links := g.nodes[from].links[layer]
	sort.SliceStable(links, func(i, j int) bool {
		left := distance(g.nodes[from].vector, g.nodes[links[i]].vector)
		right := distance(g.nodes[from].vector, g.nodes[links[j]].vector)
		return left < right
	})
	if len(links) > limit {
		links = links[:limit]
	}
	g.nodes[from].links[layer] = links
}
