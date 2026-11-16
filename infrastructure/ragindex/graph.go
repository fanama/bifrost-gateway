// Package ragindex implements the in-memory HNSW index used by RAG retrieval.
// It is a pure-Go adapter: no CGO, no external vector service, and the graph is
// rebuilt from persisted chunks instead of being persisted itself.
package ragindex

import (
	"math"
	"math/rand"

	domainrag "bridge-gateway/domain/rag"
)

type node struct {
	chunk  domainrag.Chunk
	vector []float32 // L2-normalized, so inner product is cosine similarity
	level  int
	links  [][]int32 // links[layer] holds neighbor node indexes
}

type graph struct {
	nodes          []node
	byID           map[string]int32
	entry          int32
	maxLevel       int
	m              int
	maxLinks       int
	efConstruction int
	model          string
	rng            *rand.Rand
}

func newGraph(model string, m, efConstruction int, seed int64) *graph {
	if m < 2 {
		m = 2
	}
	return &graph{
		byID:           map[string]int32{},
		entry:          -1,
		maxLevel:       -1,
		m:              m,
		maxLinks:       2 * m,
		efConstruction: efConstruction,
		model:          model,
		rng:            rand.New(rand.NewSource(seed)),
	}
}

func (g *graph) width() int {
	if len(g.nodes) == 0 {
		return 0
	}
	return len(g.nodes[0].vector)
}

// addChunks inserts every chunk from the graph's embedding model and width. A
// project's storage may contain vectors from older models; they are ignored so
// the graph never mixes embedding spaces.
func (g *graph) addChunks(chunks []domainrag.Chunk, model string) {
	width := 0
	for i := range chunks {
		chunk := chunks[i]
		if chunk.EmbeddingModel != model || len(chunk.Vector) == 0 {
			continue
		}
		if width == 0 {
			width = len(chunk.Vector)
		}
		if len(chunk.Vector) != width {
			continue
		}
		g.insert(chunk)
	}
}

func normalize(vec []float32) []float32 {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return nil
	}
	scale := float32(1 / math.Sqrt(norm))
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v * scale
	}
	return out
}

func dot(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func distance(a, b []float32) float32 {
	return float32(1 - dot(a, b))
}

// randomLevel draws a layer from the geometric distribution used by HNSW. The
// cap keeps a pathological draw from creating an enormous neighbor table.
func (g *graph) randomLevel() int {
	if g.m < 2 {
		return 0
	}
	level := int(math.Floor(-math.Log(g.rng.Float64()+1e-12) / math.Log(float64(g.m))))
	if level > 16 {
		level = 16
	}
	return level
}
