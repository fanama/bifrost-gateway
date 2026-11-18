package rag

import (
	"sort"
	"strconv"
)

// ReciprocalRankFusionRank is the rank constant of Cormack et al. It damps the
// weight of deep positions so a passage ranked first by one retriever beats a
// passage ranked second by both.
const ReciprocalRankFusionRank = 60

// ReciprocalRankFusion merges rankings by summing 1/(rankConstant+position)
// per passage. A passage present in several lists rises, and each passage is
// kept once. The fused score replaces the original one, so callers see the
// ranking they actually got.
func ReciprocalRankFusion(lists ...[]RetrievedChunk) []RetrievedChunk {
	merged := map[string]*RetrievedChunk{}
	scores := map[string]float64{}
	for _, list := range lists {
		for position, hit := range list {
			key := chunkKey(hit.Chunk)
			if _, seen := scores[key]; !seen {
				kept := hit
				merged[key] = &kept
			}
			scores[key] += 1 / float64(ReciprocalRankFusionRank+position+1)
		}
	}
	out := make([]RetrievedChunk, 0, len(merged))
	for key, hit := range merged {
		hit.Score = scores[key]
		out = append(out, *hit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return chunkKey(out[i].Chunk) < chunkKey(out[j].Chunk)
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// chunkKey identifies a passage across retrievers. Stored chunks always carry
// an ID; the fallback keeps hand-built test fixtures usable.
func chunkKey(chunk Chunk) string {
	if chunk.ID != "" {
		return chunk.ID
	}
	return chunk.DocumentID + "#" + strconv.Itoa(chunk.Ordinal)
}
