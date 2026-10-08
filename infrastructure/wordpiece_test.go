package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestVocab ecrit un vocab.txt de test : l'index de chaque token est son
// numero de ligne, exactement comme BERT.
func writeTestVocab(t *testing.T, tokens []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vocab.txt")
	content := ""
	for _, tok := range tokens {
		content += tok + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write vocab: %v", err)
	}
	return path
}

func testVocabTokens() []string {
	return []string{
		"[PAD]", "[UNK]", "[CLS]", "[SEP]",
		",", "!",
		"hello", "world", "##ing",
		"le", "chat", "dort", "sur", "canape",
	}
}

func mustTokenizer(t *testing.T, maxSeqLen int) *wordPieceTokenizer {
	t.Helper()
	tok, err := newWordPieceTokenizer(writeTestVocab(t, testVocabTokens()), maxSeqLen)
	if err != nil {
		t.Fatalf("new tokenizer: %v", err)
	}
	return tok
}

func tokensOf(t *testing.T, tok *wordPieceTokenizer, ids []int64) []string {
	t.Helper()
	reverse := make(map[int64]string, len(tok.vocab))
	for name, id := range tok.vocab {
		reverse[int64(id)] = name
	}
	var out []string
	for _, id := range ids {
		name, ok := reverse[id]
		if !ok {
			t.Fatalf("unknown id %d", id)
		}
		if name == "[PAD]" {
			break
		}
		out = append(out, name)
	}
	return out
}

func TestWordPieceEncodeWrapsWithClsSep(t *testing.T) {
	tok := mustTokenizer(t, 16)
	ids, mask, types := tok.encode("Hello, world!")

	got := tokensOf(t, tok, ids)
	want := []string{"[CLS]", "hello", ",", "world", "!", "[SEP]"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// mask : 1 sur les tokens reels, 0 sur le remplissage ; types toujours 0.
	for i := range ids {
		real := i < len(want)
		if mask[i] != boolToInt64(real) {
			t.Errorf("mask[%d] = %d, want %d", i, mask[i], boolToInt64(real))
		}
		if types[i] != 0 {
			t.Errorf("types[%d] = %d, want 0", i, types[i])
		}
	}
	if int64(tok.vocab["[PAD]"]) != ids[len(want)] {
		t.Errorf("padding must use [PAD], got id %d", ids[len(want)])
	}
}

func TestWordPieceFoldsAccentsAndCase(t *testing.T) {
	tok := mustTokenizer(t, 16)
	ids, _, _ := tok.encode("CANAPÉ")
	got := tokensOf(t, tok, ids)
	want := []string{"[CLS]", "canape", "[SEP]"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWordPieceSplitsUnknownWordIntoSubwords(t *testing.T) {
	tok := mustTokenizer(t, 16)
	// "helloing" n'est pas dans le vocabulaire : appariement glouton
	// hello + ##ing.
	ids, _, _ := tok.encode("helloing")
	got := tokensOf(t, tok, ids)
	want := []string{"[CLS]", "hello", "##ing", "[SEP]"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWordPieceUsesUnkWhenNoSplitPossible(t *testing.T) {
	tok := mustTokenizer(t, 16)
	ids, _, _ := tok.encode("xyzzy")
	got := tokensOf(t, tok, ids)
	want := []string{"[CLS]", "[UNK]", "[SEP]"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// La troncature conserve [CLS] en tete et [SEP] en queue : couper au hasard
// casserait le segment attendu par le modele.
func TestWordPieceTruncatesKeepingSepAtEnd(t *testing.T) {
	tok := mustTokenizer(t, 4)
	ids, mask, _ := tok.encode("le chat dort sur le chat dort sur le chat")
	if len(ids) != 4 {
		t.Fatalf("expected fixed length 4, got %d", len(ids))
	}
	got := tokensOf(t, tok, ids)
	if got[0] != "[CLS]" || got[len(got)-1] != "[SEP]" {
		t.Errorf("truncated tokens must keep CLS/SEP, got %v", got)
	}
	if len(got) != 4 || mask[3] != 1 {
		t.Errorf("full window should be real tokens: mask=%v tokens=%v", mask, got)
	}
}

func TestWordPieceRejectsVocabWithoutSpecialTokens(t *testing.T) {
	path := writeTestVocab(t, []string{"hello", "world"})
	if _, err := newWordPieceTokenizer(path, 16); err == nil {
		t.Fatal("expected error for vocab missing [CLS]/[SEP]/...")
	}
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
