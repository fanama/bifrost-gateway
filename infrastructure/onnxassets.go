package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Telechargement des artefacts ONNX au premier lancement.
//
// models/onnx/ est ignore par git : sur une machine neuve le repertoire est
// absent et chaque choix ONNX rendrait une erreur. Au demarrage, les fichiers
// manquants (ou tronques) sont recuperes depuis leur source canonique, un par
// un : un echec reseau n'empeche jamais le serveur de demarrer, il laisse les
// erreurs explicites existantes en place et est journalise.

const (
	onnxAutoDownloadEnv   = "ONNX_AUTO_DOWNLOAD"
	onnxRuntimeVersion    = "1.23.0"
	hfMinilmRepo          = "https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main"
	hfParaphraseRepo      = "https://huggingface.co/Xenova/paraphrase-MiniLM-L3-v2/resolve/main"
	hfSmolLMChatRepo      = "https://huggingface.co/onnx-community/SmolLM2-135M-Instruct-ONNX-GQA/resolve/main"
	onnxChatModelDir      = "chat/smollm2-135m-instruct"
	ortGitHubReleaseBase  = "https://github.com/microsoft/onnxruntime/releases/download/v" + onnxRuntimeVersion
	defaultDownloadClient = 15 * time.Minute
)

// onnxAsset decrit un artefact a mettre en place : dest est un chemin relatif
// a models/onnx/, min la taille minimale attendue (les sources sont versionnees
// par taille, un fichier plus petit est un telechargement tronque).
type onnxAsset struct {
	dest   string
	url    string
	min    int64
	member string // chemin du fichier dans l'archive (vide = fichier simple)
}

// onnxAutoDownloadEnabled lit le gate d'activation. Defaut active ; "0",
// "false", "no" ou "off" desactivent tout telechargement (artefacts fournis
// a la main ou environnement hors-ligne).
func onnxAutoDownloadEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(onnxAutoDownloadEnv)))
	switch v {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// defaultOnnxAssets est le manifeste des artefacts requis : bibliotheque
// ONNX, modeles d'embedding (chemins plats + repertoire discover) et modele de
// chat (graphe GQA + tokenizer assorti, un seul depot pour rester coherent).
func defaultOnnxAssets() []onnxAsset {
	assets := []onnxAsset{
		{dest: "model.onnx", url: hfMinilmRepo + "/onnx/model_quantized.onnx", min: 22972370},
		{dest: "vocab.txt", url: hfMinilmRepo + "/vocab.txt", min: 231508},
		{dest: "paraphrase-MiniLM-L3-v2/model.onnx", url: hfParaphraseRepo + "/onnx/model_quantized.onnx", min: 17452106},
		{dest: onnxChatModelDir + "/model.onnx", url: hfSmolLMChatRepo + "/onnx/model_quantized.onnx", min: 137146931},
		{dest: onnxChatModelDir + "/config.json", url: hfSmolLMChatRepo + "/config.json", min: 900},
		{dest: onnxChatModelDir + "/generation_config.json", url: hfSmolLMChatRepo + "/generation_config.json", min: 100},
		{dest: onnxChatModelDir + "/tokenizer.json", url: hfSmolLMChatRepo + "/tokenizer.json", min: 2000000},
		{dest: onnxChatModelDir + "/tokenizer_config.json", url: hfSmolLMChatRepo + "/tokenizer_config.json", min: 3000},
	}
	if lib, err := onnxRuntimeAsset(); err == nil {
		assets = append(assets, lib)
	}
	return assets
}

// onnxRuntimeAsset resout l'archive du runtime pour la plateforme courante.
// Unsupported est une erreur non bloquante : le manifeste est simplement prive
// de la bibliotheque, comme aujourd'hui sans telechargement.
func onnxRuntimeAsset() (onnxAsset, error) {
	var archive, member, dest string
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		archive, member, dest = "onnxruntime-osx-arm64", "lib/libonnxruntime."+onnxRuntimeVersion+".dylib", "lib/libonnxruntime.dylib"
	case runtime.GOOS == "darwin":
		archive, member, dest = "onnxruntime-osx-x86_64", "lib/libonnxruntime."+onnxRuntimeVersion+".dylib", "lib/libonnxruntime.dylib"
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		archive, member, dest = "onnxruntime-linux-aarch64", "lib/libonnxruntime.so."+onnxRuntimeVersion, "lib/libonnxruntime.so"
	case runtime.GOOS == "linux":
		archive, member, dest = "onnxruntime-linux-x64", "lib/libonnxruntime.so."+onnxRuntimeVersion, "lib/libonnxruntime.so"
	case runtime.GOOS == "windows":
		archive, member, dest = "onnxruntime-win-x64", "lib/onnxruntime.dll", "lib/onnxruntime.dll"
	default:
		return onnxAsset{}, fmt.Errorf("onnx auto-download: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	ext := ".tgz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return onnxAsset{
		dest:   dest,
		url:    ortGitHubReleaseBase + "/" + archive + "-" + onnxRuntimeVersion + ext,
		min:    1 << 20, // toute bibliotheque reelle pese au moins 1 Mo
		member: member,
	}, nil
}

// EnsureOnnxAssets telecharge les artefacts manquants sous models/onnx/.
// Point d'entree appele au demarrage : active par defaut, desactivable par
// ONNX_AUTO_DOWNLOAD=0. Retourne les echecs reunis — l'appelant journalise et
// demarre quand meme (les artefacts absents restent des erreurs explicites).
func EnsureOnnxAssets(ctx context.Context) error {
	if !onnxAutoDownloadEnabled() {
		return nil
	}
	return ensureOnnxAssets(ctx, OnnxModelsDir, defaultOnnxAssets(), defaultOnnxHTTPClient(), os.Stderr)
}

// defaultOnnxHTTPClient borne chaque telechargement (archives de quelques Mo
// a modeles de 137 Mo) sans limite globale : le ctx de l'appelant tranche.
func defaultOnnxHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultDownloadClient}
}

// ensureOnnxAssets est la partie testable : manifeste, racine, client et
// sortie sont injectes. Chaque fichier absent (ou plus petit que min) est
// telecharge vers <dest>.part puis renomme — un telechargement coupe laisse
// donc jamais un fichier final tronque pour le passer au suivant.
func ensureOnnxAssets(ctx context.Context, root string, assets []onnxAsset, client *http.Client, out io.Writer) error {
	if client == nil {
		client = defaultOnnxHTTPClient()
	}
	logf := func(format string, args ...any) {
		if out != nil {
			fmt.Fprintf(out, "[onnx] "+format+"\n", args...)
		}
	}

	var errs []error
	for _, a := range assets {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		dest := filepath.Join(root, filepath.FromSlash(a.dest))
		present, err := assetPresent(dest, a.min)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		if present {
			continue
		}
		if err := downloadAsset(ctx, client, a, dest, logf); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(ensureParaphraseVocab(root, errs)...)
}

// downloadAsset fetches one missing artifact, logging progress and failures.
func downloadAsset(ctx context.Context, client *http.Client, a onnxAsset, dest string, logf func(string, ...any)) error {
	logf("telechargement de %s (%s)", a.dest, a.url)
	start := time.Now()
	var err error
	if a.member != "" {
		err = downloadArchiveMember(ctx, client, a, dest, filepath.Dir(dest))
	} else {
		err = downloadFile(ctx, client, a, dest)
	}
	if err != nil {
		logf("echec de %s: %v", a.dest, err)
		return fmt.Errorf("%s: %w", a.dest, err)
	}
	logf("%s pret (%.1f Mo, %s)", a.dest, float64(sizeOf(dest))/(1<<20), time.Since(start).Round(time.Millisecond))
	return nil
}

// ensureParaphraseVocab reuses the default MiniLM vocabulary for the discover
// model; both BERT vocabularies are identical.
func ensureParaphraseVocab(root string, errs []error) []error {
	src := filepath.Join(root, "vocab.txt")
	dst := filepath.Join(root, "paraphrase-MiniLM-L3-v2", "vocab.txt")
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return errs
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return errs
	}
	if err := writeFileAtomic(dst, data); err != nil {
		return append(errs, err)
	}
	return errs
}

// assetPresent : le fichier existe et pese au moins min (0 = presence seule).
func assetPresent(dest string, min int64) (bool, error) {
	fi, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return fi.Size() >= min, nil
}
