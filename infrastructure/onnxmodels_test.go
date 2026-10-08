package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

// Le decouvert repose sur la convention <repertoire>/{model.onnx,vocab.txt} :
// un repertoire incomplet n'est pas un modele, les artefacts plats du modele
// par defaut non plus (ils restent portes par le choix curate), et l'ordre
// retourne est stable — c'est celui que la page Modeles affiche.
func TestListOnnxModels(t *testing.T) {
	dir := t.TempDir()
	mkdirWith := func(name string, files ...string) {
		base := filepath.Join(dir, name)
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(base, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkdirWith("zeta", "model.onnx", "vocab.txt")
	mkdirWith("alpha", "model.onnx", "vocab.txt")
	mkdirWith("sans-vocab", "model.onnx")
	mkdirWith("vide")
	mkdirWith(".cache", "model.onnx", "vocab.txt")
	// Artefacts plats du modele par defaut : un fichier n'est pas un repertoire.
	for _, f := range []string{"model.onnx", "vocab.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := ListOnnxModels(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "zeta" {
		t.Fatalf("expected [alpha zeta], got %v", ids)
	}
}

// Un repertoire absent est une erreur, pas une liste vide : c'est l'appelant
// qui decide entre « aucun modele local » et une vraie panne.
func TestListOnnxModelsMissingDir(t *testing.T) {
	if _, err := ListOnnxModels(filepath.Join(t.TempDir(), "absent")); !os.IsNotExist(err) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}
