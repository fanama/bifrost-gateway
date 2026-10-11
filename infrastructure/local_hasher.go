package infrastructure

import (
	"math"
	"strings"
	"unicode"
)

// Vectoriseur par hachage du runtime d'embedding local : features textuelles,
// projection FNV-1a signee et normalisation L2. independant de l'adaptateur
// domain.EmbeddingProvider (localembedder.go) qui l'expose.

// embedText produit un vecteur L2-normalise de dim dimensions. Un texte vide
// (autorise par l'API) produit un vecteur nul : diviser par une norme nulle
// corromprait le resultat, donc la normalisation est conditionnelle.
func embedText(text string, dim int) []float32 {
	vec := make([]float32, dim)
	for _, feature := range localFeatures(text) {
		h := fnv64a(feature)
		idx := int(h % uint64(dim))
		if h>>63 == 1 {
			vec[idx] -= 1
		} else {
			vec[idx] += 1
		}
	}

	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum > 0 {
		inv := float32(1 / math.Sqrt(sum))
		for i := range vec {
			vec[i] *= inv
		}
	}
	return vec
}

// localFeatures decoupe le texte en features de trois niveaux :
// unigrammes (w:), bigrammes adjacents (b:) et trigrammes de caracteres
// (c:) pour amortir la morphologie francaise (paisible / paisiblement).
// Le prefixe de type evite qu'un unigramme et un trigramme identiques
// partagent le meme emplacement.
func localFeatures(text string) []string {
	tokens := localTokens(text)
	features := make([]string, 0, len(tokens)*4)
	for i, tok := range tokens {
		features = append(features, "w:"+tok)
		if i > 0 {
			features = append(features, "b:"+tokens[i-1]+"\x00"+tok)
		}
		runes := []rune(tok)
		if len(runes) >= 4 {
			for j := 0; j+3 <= len(runes); j++ {
				features = append(features, "c:"+string(runes[j:j+3]))
			}
		}
	}
	return features
}

// localTokens decoupe en mots sur les lettres et chiffres, insensible a la
// casse. La decomposition est faite rune par rune pour traiter correctement
// les caracteres accents en UTF-8.
func localTokens(text string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// fnv64a calcule le hash FNV-1a 64 bits sans allocation, ce qui garde
// l'encodage d'un lot de textes lineaire en pratique.
func fnv64a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// localTokenCount estime les tokens par comptage de mots, la meme heuristique
// que handlers/estimateTokens : un client ne doit pas voir deux comptes
// differents selon le runtime qui a repondu.
func localTokenCount(text string) int {
	return len(strings.Fields(text))
}
