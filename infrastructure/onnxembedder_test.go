package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bridge-gateway/domain"
)

// Un environnement sans artefacts doit rendre une erreur explicite qui cite
// les fichiers manquants — pas un panic ni une erreur d'infrence obscure.
func TestOnnxEmbedderReportsMissingArtifacts(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	e := NewOnnxEmbedder(OnnxEmbedderOptions{
		LibraryPath: missing + ".dylib",
		ModelPath:   missing + ".onnx",
		VocabPath:   missing + ".txt",
		MaxSeqLen:   16,
	})

	_, err := e.Embed(context.Background(),
		&domain.ChatConfig{Provider: "onnx"},
		&domain.EmbeddingRequest{Input: embedJSON(t, "texte")})

	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidRequestError, got %v", err)
	}
	if invalid.Param != "model" {
		t.Errorf("expected param model, got %q", invalid.Param)
	}
	if !strings.Contains(invalid.Reason, "onnx runtime not configured") {
		t.Errorf("expected explicit not-configured reason, got %q", invalid.Reason)
	}
}

// TestOnnxEmbedderRealModel exerce la chaine complete (lib ONNX + modele +
// vocabulaire) quand les artefacts existent. Sans eux, le test est saute et
// les tests unitaires restent la reference : models/onnx/ est un assemblage
// local, pas un depot.
func TestOnnxEmbedderRealModel(t *testing.T) {
	opts := DefaultOnnxEmbedderOptions()
	// Les tests s'executent avec CWD = paquet infrastructure/ : les chemins
	// relatifs de la config sont recales a la racine du module, sinon le test
	// se silencieusement skip au lieu d'exercer la chaine reelle.
	opts.LibraryPath = fromModuleRoot(opts.LibraryPath)
	opts.ModelPath = fromModuleRoot(opts.ModelPath)
	opts.VocabPath = fromModuleRoot(opts.VocabPath)
	for label, path := range map[string]string{
		"library": opts.LibraryPath,
		"model":   opts.ModelPath,
		"vocab":   opts.VocabPath,
	} {
		if !fileExists(path) {
			t.Skipf("artefact ONNX absent (%s: %s) — voir models/onnx/", label, path)
		}
	}

	e := NewOnnxEmbedder(opts)
	cfg := &domain.ChatConfig{Provider: "onnx", Model: domain.ModelOnnxMiniLM}

	// Paires calibrees sur le modele reel : un doublon proche (similarite
	// ~0.9) contre un texte sans rapport (plancher ~0.45 pour ce modele) —
	// c'est l'ecart que le testeur affiche en score de similarite.
	resp, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input: embedJSON(t, []string{
			"le chat dort sur le canape",
			"le chat dort paisiblement sur le canape",
			"les marches boursiers ont chute cette semaine",
		}),
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if resp.Model != domain.ModelOnnxMiniLM {
		t.Errorf("model = %q, want %q", resp.Model, domain.ModelOnnxMiniLM)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.Data))
	}
	for i, item := range resp.Data {
		if item.Index != i {
			t.Errorf("item %d: index = %d", i, item.Index)
		}
		// all-MiniLM-L6-v2 produit 384 dimensions (documente dans le choix).
		if len(item.Embedding) != 384 {
			t.Fatalf("item %d: expected 384 dimensions, got %d", i, len(item.Embedding))
		}
	}

	// Le contrat sémantique : deux phrases proches doivent scorer plus haut
	// qu'un texte sans rapport — c'est la difference avec le moteur local
	// lexical, et ce que le testeur affiche.
	similar := cosineSimilarity(resp.Data[0].Embedding, resp.Data[1].Embedding)
	unrelated := cosineSimilarity(resp.Data[0].Embedding, resp.Data[2].Embedding)
	if similar <= unrelated {
		t.Errorf("expected semantic similarity: similar=%f unrelated=%f", similar, unrelated)
	}
	if similar < unrelated+0.1 {
		t.Errorf("expected a clear semantic margin: similar=%f unrelated=%f", similar, unrelated)
	}

	// Dimensions demandees superieures au modele : refus explicite.
	tooBig := 4096
	_, err = e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input:      embedJSON(t, "texte"),
		Dimensions: &tooBig,
	})
	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Param != "dimensions" {
		t.Fatalf("expected dimensions InvalidRequestError, got %v", err)
	}

	// Troncature a une dimension inferieure, vector renormalise.
	small := 64
	truncated, err := e.Embed(context.Background(), cfg, &domain.EmbeddingRequest{
		Input:      embedJSON(t, "texte"),
		Dimensions: &small,
	})
	if err != nil {
		t.Fatalf("truncate embed: %v", err)
	}
	vec := truncated.Data[0].Embedding
	if len(vec) != 64 {
		t.Fatalf("expected 64 dimensions, got %d", len(vec))
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if diff := norm - 1; diff > 1e-4 || diff < -1e-4 {
		t.Errorf("expected renormalized vector, norm = %f", norm)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fromModuleRoot recale un chemin relatif de configuration sur la racine du
// module. Un chemin absolu (variable d'environnement) est laisse tel quel.
func fromModuleRoot(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join("..", path)
}
