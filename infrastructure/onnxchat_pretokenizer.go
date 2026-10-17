package infrastructure

import (
	"strings"
	"unicode"
)

// Ce fichier contient la pre-tokenisation BPE (ByteLevel + motif GPT-2) et le
// nettoyage UTF-8 utilisees par onnxChatTokenizer. Le vocabulaire, les merges
// et le decodage vivent dans onnxchat_tokenizer.go.

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
	runes := []rune(text)
	for i := 0; i < len(runes); {
		if matched := contractionAt(runes, i); matched != "" {
			out = append(out, matched)
			i += len([]rune(matched))
			continue
		}
		end := scanGPT2Piece(runes, i)
		if end <= i {
			end = i + 1
		}
		out = append(out, string(runes[i:end]))
		i = end
	}
	return out
}

// contractionAt returns the lowercase English contraction starting at i, if any.
func contractionAt(runes []rune, i int) string {
	for _, c := range gpt2Contractions {
		if hasPrefixAt(runes, i, c) {
			return c
		}
	}
	return ""
}

// scanGPT2Piece applies the remaining ByteLevel rules in regex order.
func scanGPT2Piece(runes []rune, i int) int {
	n := len(runes)
	next := func(from int) rune {
		if from < n {
			return runes[from]
		}
		return 0
	}

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
		// Digits(individual): one digit per piece.
		j++
	case space && body != 0 && !isSpace(body) && !isLetter(body) && !isASCIIDigit(body) ||
		!space && !isSpace(runes[i]) && !isLetter(runes[i]) && !isASCIIDigit(runes[i]):
		for j < n && !isSpace(runes[j]) && !isLetter(runes[j]) && !isASCIIDigit(runes[j]) {
			j++
		}
	default:
		return scanGPTSpaces(runes, i)
	}
	return j
}

// scanGPTSpaces applies \s+(?!\S): it takes all spaces but the last when a
// non-space follows, otherwise the whole run (end of piece).
func scanGPTSpaces(runes []rune, i int) int {
	n := len(runes)
	j := i
	for j < n && isSpace(runes[j]) {
		j++
	}
	if j < n && j-i > 1 {
		j--
	}
	if j == i {
		j++
	}
	return j
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
