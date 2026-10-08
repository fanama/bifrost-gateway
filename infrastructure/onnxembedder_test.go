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

// pathsFor : le modele par defaut reste sur ses chemins plats configures,
// tout autre identifiant resout vers la convention <repertoire>/ de la
// racine des modeles, et les ids invalides (sous-chemin, remontee,
// prefixe vide) sont refuses avant toute lecture disque.
func TestOnnxEmbedderPathsFor(t *testing.T) {
	e := NewOnnxEmbedder(OnnxEmbedderOptions{
		ModelPath: filepath.Join("preset", "model.onnx"),
		VocabPath: filepath.Join("preset", "vocab.txt"),
	})

	for _, id := range []string{domain.ModelOnnxMiniLM, "", "  "} {
		mv, vv, err := e.pathsFor(id)
		if err != nil {
			t.Fatalf("default %q: %v", id, err)
		}
		if mv != filepath.Join("preset", "model.onnx") || vv != filepath.Join("preset", "vocab.txt") {
			t.Fatalf("default %q must use configured paths, got %q %q", id, mv, vv)
		}
	}

	wantModel := filepath.Join(OnnxModelsDir, "paraphrase-MiniLM-L3-v2", "model.onnx")
	wantVocab := filepath.Join(OnnxModelsDir, "paraphrase-MiniLM-L3-v2", "vocab.txt")
	for _, id := range []string{"onnx/paraphrase-MiniLM-L3-v2", "paraphrase-MiniLM-L3-v2"} {
		mv, vv, err := e.pathsFor(id)
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		if mv != wantModel || vv != wantVocab {
			t.Fatalf("%q: got %q %q, want %q %q", id, mv, vv, wantModel, wantVocab)
		}
	}

	for _, id := range []string{"onnx/", "onnx/..", "onnx/../secret", "onnx/a/b", "autre/a"} {
		_, _, err := e.pathsFor(id)
		var invalid *domain.InvalidRequestError
		if !errors.As(err, &invalid) || invalid.Param != "model" {
			t.Fatalf("%q: expected InvalidRequestError on model, got %v", id, err)
		}
	}
}

// Un identifiant inconnu cite le chemin attendu : l'utilisateur voit quel
// repertoire creer plutot qu'une erreur generique. Le test exige la
// bibliotheque (sinon c'est elle qui manque, et le message est juste autre).
func TestOnnxEmbedderUnknownModelCitesExpectedPath(t *testing.T) {
	opts := DefaultOnnxEmbedderOptions()
	opts.LibraryPath = fromModuleRoot(opts.LibraryPath)
	opts.ModelPath = fromModuleRoot(opts.ModelPath)
	opts.VocabPath = fromModuleRoot(opts.VocabPath)
	opts.ModelsDir = fromModuleRoot(OnnxModelsDir)
	if !fileExists(opts.LibraryPath) {
		t.Skipf("librairie ONNX absente (%s) — voir models/onnx/", opts.LibraryPath)
	}

	e := NewOnnxEmbedder(opts)
	_, err := e.Embed(context.Background(),
		&domain.ChatConfig{Provider: "onnx"},
		&domain.EmbeddingRequest{Input: embedJSON(t, "x"), Model: "onnx/ghost-model"})
	var invalid *domain.InvalidRequestError
	if !errors.As(err, &invalid) || invalid.Param != "model" {
		t.Fatalf("expected InvalidRequestError on model, got %v", err)
	}
	want := filepath.Join(opts.ModelsDir, "ghost-model", "model.onnx")
	if !strings.Contains(invalid.Reason, want) {
		t.Errorf("reason must cite %q, got %q", want, invalid.Reason)
	}
}

// Deux modeles locaux cohabitent dans le meme embedder : sessions distinctes
// (cache par identifiant), meme texte vectorise differemment par chacun,
// vecteurs normalises. Sans artefacts du second modele, le test est saute.
func TestOnnxEmbedderMultipleModels(t *testing.T) {
	opts := DefaultOnnxEmbedderOptions()
	opts.LibraryPath = fromModuleRoot(opts.LibraryPath)
	opts.ModelPath = fromModuleRoot(opts.ModelPath)
	opts.VocabPath = fromModuleRoot(opts.VocabPath)
	opts.ModelsDir = fromModuleRoot(OnnxModelsDir)
	second := filepath.Join(opts.ModelsDir, "paraphrase-MiniLM-L3-v2")
	for _, path := range []string{
		opts.LibraryPath, opts.ModelPath, opts.VocabPath,
		filepath.Join(second, "model.onnx"), filepath.Join(second, "vocab.txt"),
	} {
		if !fileExists(path) {
			t.Skipf("artefact ONNX absent (%s) — voir models/onnx/", path)
		}
	}

	e := NewOnnxEmbedder(opts)
	ctx := context.Background()
	cfg := &domain.ChatConfig{Provider: "onnx"}
	input := &domain.EmbeddingRequest{Input: embedJSON(t, "le chat dort paisiblement sur le canape")}

	input.Model = domain.ModelOnnxMiniLM
	first, err := e.Embed(ctx, cfg, input)
	if err != nil {
		t.Fatalf("default model: %v", err)
	}
	// Un deuxieme appel reutilise la session : le cache est par identifiant.
	if _, err := e.Embed(ctx, cfg, input); err != nil {
		t.Fatalf("default model again: %v", err)
	}
	if len(e.sessions) != 1 {
		t.Fatalf("expected 1 cached session after repeat, got %d", len(e.sessions))
	}

	input.Model = "onnx/paraphrase-MiniLM-L3-v2"
	secondResp, err := e.Embed(ctx, cfg, input)
	if err != nil {
		t.Fatalf("second model: %v", err)
	}
	if len(e.sessions) != 2 {
		t.Fatalf("expected 2 cached sessions, got %d", len(e.sessions))
	}
	if secondResp.Model != "onnx/paraphrase-MiniLM-L3-v2" {
		t.Errorf("echo model = %q", secondResp.Model)
	}

	vecA, vecB := first.Data[0].Embedding, secondResp.Data[0].Embedding
	if len(vecA) == 0 || len(vecB) == 0 {
		t.Fatal("empty vector")
	}
	identical := len(vecA) == len(vecB)
	if identical {
		for i := range vecA {
			if vecA[i] != vecB[i] {
				identical = false
				break
			}
		}
	}
	if identical {
		t.Error("two distinct models must not produce identical vectors for the same text")
	}
	for name, vec := range map[string][]float32{"default": vecA, "second": vecB} {
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		if diff := norm - 1; diff > 1e-4 || diff < -1e-4 {
			t.Errorf("%s vector not normalized, norm = %f", name, norm)
		}
	}
}
