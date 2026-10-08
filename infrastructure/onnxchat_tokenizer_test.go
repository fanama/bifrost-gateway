package infrastructure

import (
	"path/filepath"
	"testing"
)

// chatTokFromRoot charge le tokenizer du modele de chat depuis la racine du
// module (CWD des tests = paquet infrastructure/).
func chatTokFromRoot(t *testing.T) *onnxChatTokenizer {
	t.Helper()
	path := fromModuleRoot(filepath.Join("models", "onnx", "chat", "smollm2-135m-instruct", "tokenizer.json"))
	tok, err := newOnnxChatTokenizer(path)
	if err != nil {
		t.Skipf("tokenizer absent — voir models/onnx/chat/: %v", err)
	}
	return tok
}

// decode(ids) reconstruit le texte : vocabulaire inverse puisannulation du
// mapping byte-level.
func (t *onnxChatTokenizer) decode(ids []int) string {
	rev := make(map[int]string, len(t.vocab))
	for s, id := range t.vocab {
		rev[id] = s
	}
	var out []byte
	for _, id := range ids {
		mapped := rev[id]
		for _, r := range mapped {
			for b := 0; b < 256; b++ {
				if t.byteTable[b] == r {
					out = append(out, byte(b))
					break
				}
			}
		}
	}
	return string(out)
}

// L'aller-retour encode/decode doit rendre le texte d'origine : il detecte un
// pre-tokenizer ou des merges fausseres (perte de tokens, decoupages aberrants).
func TestChatTokenizerRoundTrip(t *testing.T) {
	tok := chatTokFromRoot(t)
	for _, text := range []string{
		"Hello world!",
		"Les marches boursiers ont chute de 12.5% en 2023.",
		"  espaces   et\tnewlines\nau milieu",
		"don't can't I'm we've",
		"emoji ok: caf\u00e9 \u00e0\u00e7 \u00e9 -- sans accent",
	} {
		ids := tok.Encode(text)
		if len(ids) == 0 {
			t.Fatalf("empty encoding for %q", text)
		}
		got := tok.decode(ids)
		if got != text {
			t.Errorf("round trip mismatch:\n want %q\n  got %q\n  ids %v", text, got, ids)
		}
	}
}

// Les tokens speciaux doivent rester atomiques (id du JSON, jamais decoupes).
func TestChatTokenizerSpecials(t *testing.T) {
	tok := chatTokFromRoot(t)
	ids := tok.Encode("<|im_start|>user")
	if len(ids) < 2 || ids[0] != tok.vocab["<|im_start|>"] {
		t.Fatalf("expected leading <|im_start|> token, got %v", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("special must not be split: %v", ids)
	}
}
