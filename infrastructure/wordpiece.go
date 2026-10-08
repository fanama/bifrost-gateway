package infrastructure

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// wordPieceTokenizer est le tokenizeur WordPiece BERT, encodage uncased :
// decomposition NFD des accents, passage en minuscules, decoupage sur la
// ponctuation, puis appariement glouton le plus long possible dans le
// vocabulaire (avec les suffixes "##").
//
// Il est volontairement en Go pur et independant du runtime ONNX : le
// vocabulaire est un simple fichier texte, et le testeur comme l'API ont
// besoin du meme encodage que le modele a vu pendant son entrainement.
type wordPieceTokenizer struct {
	vocab     map[string]int32
	padID     int32
	unkID     int32
	clsID     int32
	sepID     int32
	maxSeqLen int
}

// newWordPieceTokenizer charge un vocab.txt (un token par ligne, l'index est
// le numero de ligne). Les quatre tokens speciaux BERT sont obligatoires :
// sans eux l'encodage serait arbitrairement faux plutot que clairement en
// echec.
func newWordPieceTokenizer(path string, maxSeqLen int) (*wordPieceTokenizer, error) {
	if maxSeqLen <= 0 {
		maxSeqLen = 256
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open vocab: %w", err)
	}
	defer f.Close()

	t := &wordPieceTokenizer{vocab: make(map[string]int32, 32*1024), maxSeqLen: maxSeqLen}
	scanner := bufio.NewScanner(f)
	// Certains vocabulaires ont des lignes longues (pieces de texte) : le
	// buffer par defaut de bufio (64 Ko) est largement suffisant, mais on le
	// leve pour rester robuste a un vocabulaire exotique.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	idx := int32(0)
	for scanner.Scan() {
		token := scanner.Text()
		if _, dup := t.vocab[token]; !dup {
			t.vocab[token] = idx
		}
		idx++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read vocab: %w", err)
	}

	for name, target := range map[string]*int32{
		"[PAD]": &t.padID,
		"[UNK]": &t.unkID,
		"[CLS]": &t.clsID,
		"[SEP]": &t.sepID,
	} {
		id, ok := t.vocab[name]
		if !ok {
			return nil, fmt.Errorf("vocab %q missing required token %q", path, name)
		}
		*target = id
	}
	return t, nil
}

// encode transforme un texte en triplets (ids, attention mask, token types)
// prets pour le modele : [CLS] tokens... [SEP], tronque a maxSeqLen, puis
// padde avec [PAD] (mask 0 pour que le modele ignore le remplissage).
func (t *wordPieceTokenizer) encode(text string) (ids, mask, types []int64) {
	words := basicTokenize(text)

	tokens := make([]string, 0, len(words)+2)
	tokens = append(tokens, "[CLS]")
	for _, word := range words {
		pieces, ok := t.wordpiece(word)
		if !ok {
			tokens = append(tokens, "[UNK]")
			continue
		}
		tokens = append(tokens, pieces...)
	}
	tokens = append(tokens, "[SEP]")

	// Troncature en conservant [CLS] en tete et [SEP] en queue : c'est la
	// forme que le modele attend, une coupe au hasard casserait le segment.
	if len(tokens) > t.maxSeqLen {
		truncated := make([]string, 0, t.maxSeqLen)
		truncated = append(truncated, tokens[:t.maxSeqLen-1]...)
		truncated = append(truncated, "[SEP]")
		tokens = truncated
	}

	ids = make([]int64, t.maxSeqLen)
	mask = make([]int64, t.maxSeqLen)
	types = make([]int64, t.maxSeqLen)
	for i := 0; i < t.maxSeqLen; i++ {
		if i < len(tokens) {
			ids[i] = int64(t.vocab[tokens[i]])
			mask[i] = 1
		} else {
			ids[i] = int64(t.padID)
		}
	}
	return ids, mask, types
}

// wordpiece realise l'appariement glouton d'un mot : de gauche a droite, la
// plus longue piece presente dans le vocabulaire est retenue ; une piece non
// reconnue rend ok=false et l'appelant substitue [UNK].
func (t *wordPieceTokenizer) wordpiece(word string) ([]string, bool) {
	if word == "" {
		return nil, false
	}
	runes := []rune(word)
	if _, ok := t.vocab[word]; ok {
		return []string{word}, true
	}

	pieces := make([]string, 0, 4)
	start := 0
	for start < len(runes) {
		end := len(runes)
		matched := ""
		for end > start {
			sub := string(runes[start:end])
			if start > 0 {
				sub = "##" + sub
			}
			if _, ok := t.vocab[sub]; ok {
				matched = sub
				break
			}
			end--
		}
		if matched == "" {
			return nil, false
		}
		pieces = append(pieces, matched)
		start = end
	}
	return pieces, true
}

// basicTokenize prepare le texte comme BERT l'a vu a l'entrainement :
// decomposition NFD puis suppression des accents, minuscules, decoupage sur
// les lettres/chiffres, la ponctuation devenant des tokens isoles et les
// espaces etant abandones.
func basicTokenize(text string) []string {
	normalized := stripAccentsLower(text)
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range normalized {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			cur = append(cur, r)
		default:
			flush()
			if !unicode.IsSpace(r) {
				words = append(words, string(r))
			}
		}
	}
	flush()
	return words
}

// stripAccentsLower realise la normalisation uncased de BERT : NFD expose
// les accents comme sequences lettre + combine (Mn), que l'on ecarte avant
// de ramener le texte en minuscules. Le decomposeur NFD de la stdlib n'existe
// pas : x/text/unicode/norm est deja une dependance du module.
func stripAccentsLower(text string) string {
	decomposed := norm.NFD.String(text)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
