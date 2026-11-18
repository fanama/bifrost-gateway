package rag

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 constants. They are deliberately fixed: the pipeline exposes the
// strategy, not the scoring constants.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Tokenize splits text into lowercase terms, dropping punctuation. A hyphen or
// a slash separates terms, so an identifier such as "ORION-42" stays findable
// from either side.
func Tokenize(text string) []string {
	terms := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i := range terms {
		terms[i] = strings.ToLower(terms[i])
	}
	return terms
}

// LexicalSearch scores every passage with BM25 against the query terms and
// returns the best limit passages. Passages sharing no term with the query are
// dropped: they carry no lexical evidence.
func LexicalSearch(query string, chunks []Chunk, limit int) []RetrievedChunk {
	terms := Tokenize(query)
	if len(terms) == 0 || len(chunks) == 0 {
		return nil
	}
	documents, average := bm25Documents(chunks)
	scored := make([]RetrievedChunk, 0, len(documents))
	for _, document := range documents {
		score := bm25Score(document, terms, documents, average)
		if score > 0 {
			scored = append(scored, RetrievedChunk{Chunk: document.chunk, Score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

type bm25Document struct {
	chunk  Chunk
	counts map[string]int
	length int
}

// bm25Documents counts terms once for the whole corpus, and returns the corpus
// documents with their average length so each score reuses the same statistics.
func bm25Documents(chunks []Chunk) ([]bm25Document, float64) {
	documents := make([]bm25Document, 0, len(chunks))
	total := 0
	for _, chunk := range chunks {
		counts := map[string]int{}
		for _, term := range Tokenize(chunk.Text) {
			counts[term]++
		}
		length := 0
		for _, count := range counts {
			length += count
		}
		total += length
		documents = append(documents, bm25Document{chunk: chunk, counts: counts, length: length})
	}
	if len(documents) == 0 {
		return nil, 0
	}
	return documents, float64(total) / float64(len(documents))
}

// bm25Score is the standard BM25 sum over query terms. The document frequency
// is recomputed from the corpus each time, which keeps the function pure.
func bm25Score(document bm25Document, terms []string, corpus []bm25Document, average float64) float64 {
	if average == 0 {
		return 0
	}
	var score float64
	for _, term := range terms {
		frequency := float64(document.counts[term])
		if frequency == 0 {
			continue
		}
		documentFrequency := 0
		for _, other := range corpus {
			if other.counts[term] > 0 {
				documentFrequency++
			}
		}
		score += bm25Term(frequency, document.length, average, documentFrequency, len(corpus))
	}
	return score
}

func bm25Term(frequency float64, length int, average float64, documentFrequency, corpusSize int) float64 {
	idf := math.Log(1 + (float64(corpusSize)-float64(documentFrequency)+0.5)/(float64(documentFrequency)+0.5))
	denominator := frequency + bm25K1*(1-bm25B+bm25B*float64(length)/average)
	return idf * frequency * (bm25K1 + 1) / denominator
}
