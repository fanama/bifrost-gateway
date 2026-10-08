package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

// onnxChatTokenizer est un tokenizer BPE pur Go, lu depuis tokenizer.json :
// vocabulaire, merges, tokens speciaux, pre-tokenizer Digits+ByteLevel du
// modele de chat. Aucune dependance externe — le meme soclet purego sans CGO
// que le reste du runtime ONNX.
type onnxChatTokenizer struct {
	vocab     map[string]int
	byID      map[int]string // vocabulaire inverse, pour Decode
	unbyte    map[rune]byte  // mapping rune -> byte (ByteLevel inverse)
	ranks     map[string]int // "gauche\x00droite" -> rang du merge
	specials  []string       // contenus speciaux, les plus longs d'abord
	byteTable [256]rune      // mapping byte -> rune (ByteLevel)
}

type chatTokenizerJSON struct {
	Model struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []string       `json:"merges"`
	} `json:"model"`
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
		Special bool   `json:"special"`
	} `json:"added_tokens"`
}

// newOnnxChatTokenizer charge et valide tokenizer.json. Seul le format BPE
// est accepte : un autre format est une erreur explicite, pas un encodage
// approximatif qui produirait des prompts muets.
func newOnnxChatTokenizer(path string) (*onnxChatTokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("onnx chat tokenizer: %w", err)
	}
	var doc chatTokenizerJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("onnx chat tokenizer: %s: %w", path, err)
	}
	if doc.Model.Type != "BPE" {
		return nil, fmt.Errorf("onnx chat tokenizer: unsupported model type %q (want BPE)", doc.Model.Type)
	}
	if len(doc.Model.Vocab) == 0 || len(doc.Model.Merges) == 0 {
		return nil, fmt.Errorf("onnx chat tokenizer: empty vocab or merges in %s", path)
	}
	t := &onnxChatTokenizer{
		vocab: doc.Model.Vocab,
		ranks: make(map[string]int, len(doc.Model.Merges)),
	}
	for i, m := range doc.Model.Merges {
		left, right, ok := strings.Cut(m, " ")
		if !ok {
			return nil, fmt.Errorf("onnx chat tokenizer: malformed merge %q", m)
		}
		t.ranks[left+"\x00"+right] = i
	}
	for _, at := range doc.AddedTokens {
		if at.Content == "" {
			continue
		}
		t.specials = append(t.specials, at.Content)
		if _, exists := t.vocab[at.Content]; !exists {
			t.vocab[at.Content] = at.ID
		}
	}
	sort.Slice(t.specials, func(i, j int) bool { return len(t.specials[i]) > len(t.specials[j]) })
	buildByteTable(&t.byteTable)
	t.unbyte = make(map[rune]byte, 256)
	for b := 0; b < 256; b++ {
		t.unbyte[t.byteTable[b]] = byte(b)
	}
	t.byID = make(map[int]string, len(t.vocab))
	for tok, id := range t.vocab {
		if _, dup := t.byID[id]; !dup {
			t.byID[id] = tok
		}
	}
	return t, nil
}

// Decode inverse Encode : les ids sont rendus en octets via le mapping
// ByteLevel inverse. Les tokens speciaux (marqueurs de chat, BOS/EOS) sont
// ecartes — le texte genere ne doit pas re-echapper ses propres delimiteurs.
// Un fragment dont aucun id n'est connu est ignore, jamais remplace par du
// texte devine.
func (t *onnxChatTokenizer) Decode(ids []int) string {
	var buf []byte
	for _, id := range ids {
		tok, ok := t.byID[id]
		if !ok || t.isSpecialContent(tok) {
			continue
		}
		for _, r := range tok {
			if b, seen := t.unbyte[r]; seen {
				buf = append(buf, b)
				continue
			}
			// Hors table ByteLevel : on recopie le rune tel quel (UTF-8)
			// plutot que de perdre l'information.
			buf = append(buf, string(r)...)
		}
	}
	return string(buf)
}

// DecodeStable decode en retenant l'eventuel character UTF-8 coupe en deux
// par un token : le morceau incomplet sera complete par le token suivant,
// un flux qui emettrait des octets orphelins ferait sauter l'affichage client.
func (t *onnxChatTokenizer) DecodeStable(ids []int) string {
	return trimIncompleteUTF8(t.Decode(ids))
}

// trimIncompleteUTF8 retranche un caractere UTF-8 tronque en fin de chaine.
func trimIncompleteUTF8(s string) string {
	n := len(s)
	if n == 0 {
		return s
	}
	i := n - 1
	for i >= 0 && s[i]&0xC0 == 0x80 {
		i--
	}
	if i < 0 {
		return s
	}
	var size int
	switch lead := s[i]; {
	case lead < 0x80:
		size = 1
	case lead&0xE0 == 0xC0:
		size = 2
	case lead&0xF0 == 0xE0:
		size = 3
	case lead&0xF8 == 0xF0:
		size = 4
	default:
		return s[:i]
	}
	if n-i < size {
		return s[:i]
	}
	return s
}

// buildByteTable construit le mapping byte -> rune de ByteLevel (GPT-2) :
// les octets visibles restent tels quels, les autres sont esquives en U+0100+
// pour que chaque octet devienne un rune distinct en entree du BPE.
func buildByteTable(table *[256]rune) {
	const printable = "!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
	// Cle par valeur d'octet, pas par position : ' ' (0x20) doit donner
	// U+0120 (et non le 33e caractere imprimable), sinon aucune cle de
	// merges n'egale jamais le vocabulaire et le BPE ne fusionne rien.
	for b := range table {
		table[b] = -1
	}
	for _, r := range printable {
		table[r] = r
	}
	for r := rune(161); r <= 172; r++ {
		table[r] = r
	}
	for r := rune(174); r <= 255; r++ {
		table[r] = r
	}
	next := rune(256)
	for b := 0; b < 256; b++ {
		if table[b] == -1 {
			table[b] = next
			next++
		}
	}
}

// Encode transforme un texte en ids, exactement dans l'ordre : tokens
// speciaux reconnus d'abord (ils ne doivent jamais etre decoupes en sous-mots),
// puis pre-tokenisation Digits -> ByteLevel, puis BPE sur chaque fragment.
func (t *onnxChatTokenizer) Encode(text string) []int {
	var ids []int
	for _, chunk := range t.splitSpecials(text) {
		if id, isSpecial := t.vocab[chunk]; isSpecial && t.isSpecialContent(chunk) {
			ids = append(ids, id)
			continue
		}
		for _, fragment := range t.preTokenize(chunk) {
			ids = append(ids, t.bpe(fragment)...)
		}
	}
	return ids
}

// isSpecialContent verifie qu'une chaine est un token special declare : un
// fragment courant qui happen etre dans le vocab n'a pas ce statut.
func (t *onnxChatTokenizer) isSpecialContent(s string) bool {
	for _, sp := range t.specials {
		if sp == s {
			return true
		}
	}
	return false
}

// splitSpecials decoupe le texte autour des tokens speciaux (les plus longs
// d'abord, pour que <|im_start|> gagne face a <|im_start|>). Les morceaux
// non-speciaux restent entiers : leur pre-tokenisation viens apres.
func (t *onnxChatTokenizer) splitSpecials(text string) []string {
	if len(t.specials) == 0 {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		best, bestAt := -1, -1
		for i, sp := range t.specials {
			at := strings.Index(text, sp)
			if at < 0 {
				continue
			}
			if bestAt == -1 || at < bestAt || (at == bestAt && i < best) {
				best, bestAt = i, at
			}
		}
		if bestAt == -1 {
			chunks = append(chunks, text)
			break
		}
		if bestAt > 0 {
			chunks = append(chunks, text[:bestAt])
		}
		chunks = append(chunks, t.specials[best])
		text = text[bestAt+len(t.specials[best]):]
	}
	return chunks
}

// preTokenize reproduit la sequence du modele : Digits(individual) coupe les
// suites de chiffres en chiffres isoles, puis ByteLevel(use_regex) applique le
// motif GPT-2. Go/RE2 ne supporte ni le look-ahead (?!S) ni ce motif en
// alternance, donc le decoupage est un scanner manuel qui respecte
// l'ordre de precedence du regex : contractions, ' ?lettres, ' ?chiffre,
// ' ?symboles, blancs (tous sauf le dernier collent au mot suivant).
func (t *onnxChatTokenizer) preTokenize(text string) []string {
	pieces := splitIndividualDigits(text)
	var out []string
	for _, piece := range pieces {
		out = append(out, scanGPT2Pattern(piece)...)
	}
	return out
}

// splitIndividualDigits applique Digits(individual_digits=true) : chaque
// chiffre devient son propre fragment, le reste reste en segments entiers.
func splitIndividualDigits(text string) []string {
	var pieces []string
	var cur strings.Builder
	sawDigit := false
	flush := func() {
		if cur.Len() > 0 {
			pieces = append(pieces, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		isDigit := r >= '0' && r <= '9'
		if isDigit {
			if sawDigit {
				flush()
			}
			cur.WriteRune(r)
			flush()
			sawDigit = true
			continue
		}
		sawDigit = false
		cur.WriteRune(r)
	}
	flush()
	return pieces
}

var gpt2Contractions = []string{"'s", "'t", "'re", "'ve", "'m", "'ll", "'d"}

// scanGPT2Pattern decoupe un fragment selon le motif ByteLevel. Les regles
// sont testees dans l'ordre du regex d'origine.
func scanGPT2Pattern(text string) []string {
	var out []string
	i := 0
	runes := []rune(text)
	n := len(runes)
	next := func(from int) rune {
		if from < n {
			return runes[from]
		}
		return 0
	}
	for i < n {
		// 1. contractions minuscules : 's 't 're 've 'm 'll 'd
		matched := ""
		for _, c := range gpt2Contractions {
			if hasPrefixAt(runes, i, c) {
				matched = c
				break
			}
		}
		if matched != "" {
			out = append(out, matched)
			i += len([]rune(matched))
			continue
		}

		start := i
		space := runes[i] == ' '
		j := i
		if space {
			j++
		}
		body := next(j)
		switch {
		case space && isLetter(body) || !space && isLetter(runes[i]):
			for j < n && isLetter(runes[j]) {
				j++
			}
		case space && isASCIIDigit(body) || !space && isASCIIDigit(runes[i]):
			// Digits(individual) : un seul chiffre par fragment.
			j++
		case space && body != 0 && !isSpace(body) && !isLetter(body) && !isASCIIDigit(body) ||
			!space && !isSpace(runes[i]) && !isLetter(runes[i]) && !isASCIIDigit(runes[i]):
			for j < n && !isSpace(runes[j]) && !isLetter(runes[j]) && !isASCIIDigit(runes[j]) {
				j++
			}
		default:
			// Blancs : \s+(?!\S) prend tous sauf le dernier quand un
			// non-blanc suit ; sinon toute la suite (fin de fragment).
			j = i
			for j < n && isSpace(runes[j]) {
				j++
			}
			if j < n && j-i > 1 {
				j-- // le dernier blanc s'attachera au mot suivant
			}
			if j == i {
				j++ // \s+ : un seul blanc sans suite favorable
			}
			out = append(out, string(runes[start:j]))
			i = j
			continue
		}
		out = append(out, string(runes[start:j]))
		i = j
	}
	return out
}

func hasPrefixAt(runes []rune, at int, prefix string) bool {
	for k, r := range prefix {
		if at+k >= len(runes) || runes[at+k] != r {
			return false
		}
	}
	return len(prefix) > 0
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127 && unicode.IsLetter(r)
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || unicode.IsSpace(r)
}

// bpe applique les merges par rang croissant sur un fragment deja vu par le
// scanner (pre-byte-map). Chaque octet est d'abord mape sur son rune
// ByteLevel, puis la paire au meilleur rang est fusionnee jusqu'au fixpoint.
func (t *onnxChatTokenizer) bpe(fragment string) []int {
	if fragment == "" {
		return nil
	}
	// Fragment d'un seul octet : directement dans le vocabulaire.
	if len(fragment) == 1 {
		mapped := string(t.byteTable[fragment[0]])
		if id, ok := t.vocab[mapped]; ok {
			return []int{id}
		}
	}
	tokens := make([]string, 0, len(fragment))
	for i := 0; i < len(fragment); i++ {
		tokens = append(tokens, string(t.byteTable[fragment[i]]))
	}
	for len(tokens) > 1 {
		best, bestRank := -1, int(^uint(0)>>1)
		for i := 0; i+1 < len(tokens); i++ {
			if rank, ok := t.ranks[tokens[i]+"\x00"+tokens[i+1]]; ok && rank < bestRank {
				best, bestRank = i, rank
			}
		}
		if best == -1 {
			break
		}
		tokens[best] = tokens[best] + tokens[best+1]
		tokens = append(tokens[:best+1], tokens[best+2:]...)
	}
	ids := make([]int, 0, len(tokens))
	for _, tok := range tokens {
		id, ok := t.vocab[tok]
		if !ok {
			// Un fragment sans entree vocabulaire : on l'omet plutot que de
			// produire un id invente que le modele interpreterait mal.
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
