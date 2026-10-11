package infrastructure

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Ce fichier regroupe la configuration partagee par tous les moteurs ONNX
// locaux (embedding et chat) : version d'API attendue, racine des modeles,
// resolution des chemins depuis l'environnement.

const (
	// onnxAPIVersion aligne l'interface C chargee par purego sur la
	// bibliotheque telechargee (ONNX Runtime 1.23.x). Une version
	// differente est refusee au chargement avec un message explicite.
	onnxAPIVersion = 23
	// onnxDefaultMaxSeqLen est la longueur fixe de sequence : padding et
	// troncature a cette taille, le masque d'attention ecarte le remplissage.
	onnxDefaultMaxSeqLen = 128
	// OnnxModelsDir est la racine des modeles ONNX locaux : chaque
	// repertoire contenant model.onnx et vocab.txt y devient un modele
	// selectable sous l'id "onnx/<repertoire>". Constante exportee pour que
	// le decouverture (main) et l'embedder parlent du meme chemin.
	OnnxModelsDir = "models/onnx"
)

// OnnxEmbedderOptions localise les trois artefacts du runtime ONNX local.
// Chaque chemin surchargeable par variable d'environnement, avec un defaut
// sous models/onnx/ : rien n'est embarque dans le binaire, mais rien n'est
// non plus installe sur le systeme.
type OnnxEmbedderOptions struct {
	LibraryPath string // libonnxruntime.{dylib,so,dll}
	ModelPath   string // fichier .onnx du modele par defaut
	VocabPath   string // vocab.txt WordPiece du modele par defaut
	// ModelsDir est la racine des modeles discovers (repertoires
	// <id>/model.onnx + vocab.txt). Surchargeable pour les tests, qui
	// s'executent avec un CWD different de la racine du module.
	ModelsDir string
	MaxSeqLen int
}

// DefaultOnnxEmbedderOptions resout la configuration depuis l'environnement.
func DefaultOnnxEmbedderOptions() OnnxEmbedderOptions {
	return OnnxEmbedderOptions{
		LibraryPath: envOrDefault("ONNX_RUNTIME_LIB", defaultOnnxLibraryPath()),
		ModelPath:   envOrDefault("ONNX_MODEL_PATH", filepath.Join(OnnxModelsDir, "model.onnx")),
		VocabPath:   envOrDefault("ONNX_VOCAB_PATH", filepath.Join(OnnxModelsDir, "vocab.txt")),
		ModelsDir:   OnnxModelsDir,
		MaxSeqLen:   envIntOrDefault("ONNX_MAX_SEQ_LEN", onnxDefaultMaxSeqLen),
	}
}

func defaultOnnxLibraryPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(OnnxModelsDir, "lib", "onnxruntime.dll")
	case "linux":
		return filepath.Join(OnnxModelsDir, "lib", "libonnxruntime.so")
	default:
		return filepath.Join(OnnxModelsDir, "lib", "libonnxruntime.dylib")
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
