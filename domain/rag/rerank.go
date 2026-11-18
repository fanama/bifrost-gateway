package rag

import "sort"

// MaximalMarginalRelevance reranks hits by relevance minus redundancy:
// lambda*relevance - (1-lambda)*max similarity to an already selected passage.
// Relevance is the min-max normalized retrieval score, so the formula works on
// cosine similarities as well as on fused rank scores.
func MaximalMarginalRelevance(query []float32, hits []RetrievedChunk, limit int, lambda float64) []RetrievedChunk {
	if limit <= 0 {
		limit = DefaultTopK
	}
	lambda = clampLambda(lambda)
	candidates := make([]mmrCandidate, len(hits))
	relevance := normalizedScores(hits)
	for i := range hits {
		candidates[i] = mmrCandidate{hit: hits[i], relevance: relevance[i]}
	}
	selected := make([]RetrievedChunk, 0, limit)
	for len(selected) < limit && len(candidates) > 0 {
		best, bestScore := -1, 0.0
		for i, candidate := range candidates {
			score := mmrScore(candidate, selected, lambda)
			if best < 0 || score > bestScore {
				best, bestScore = i, score
			}
		}
		selected = append(selected, candidates[best].hit)
		candidates = append(candidates[:best], candidates[best+1:]...)
	}
	return selected
}

type mmrCandidate struct {
	hit       RetrievedChunk
	relevance float64
}

func mmrScore(candidate mmrCandidate, selected []RetrievedChunk, lambda float64) float64 {
	redundancy := 0.0
	for _, chosen := range selected {
		similarity := CosineSimilarity(candidate.hit.Chunk.Vector, chosen.Chunk.Vector)
		if similarity > redundancy {
			redundancy = similarity
		}
	}
	return lambda*candidate.relevance - (1-lambda)*redundancy
}

// normalizedScores maps the retrieval scores to [0,1]. When every score is
// equal, relevance carries no signal and every passage gets 1, leaving the
// diversity term in charge.
func normalizedScores(hits []RetrievedChunk) []float64 {
	scores := make([]float64, len(hits))
	if len(hits) == 0 {
		return scores
	}
	low, high := hits[0].Score, hits[0].Score
	for _, hit := range hits {
		if hit.Score < low {
			low = hit.Score
		}
		if hit.Score > high {
			high = hit.Score
		}
	}
	for i, hit := range hits {
		if high == low {
			scores[i] = 1
			continue
		}
		scores[i] = (hit.Score - low) / (high - low)
	}
	return scores
}

func clampLambda(lambda float64) float64 {
	if lambda < MinMMRLambda || lambda > MaxMMRLambda {
		return DefaultMMRLambda
	}
	return lambda
}

// SortByScore orders a copy of hits by descending score, keeping the input
// order for ties. Retrieval adapters rely on it so their output is stable.
func SortByScore(hits []RetrievedChunk) []RetrievedChunk {
	sorted := make([]RetrievedChunk, len(hits))
	copy(sorted, hits)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	return sorted
}
