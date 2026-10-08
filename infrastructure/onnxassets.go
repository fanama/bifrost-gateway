package infrastructure

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
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
		logf("telechargement de %s (%s)", a.dest, a.url)
		start := time.Now()
		if a.member != "" {
			err = downloadArchiveMember(ctx, client, a, dest, filepath.Dir(dest))
		} else {
			err = downloadFile(ctx, client, a, dest)
		}
		if err != nil {
			logf("echec de %s: %v", a.dest, err)
			errs = append(errs, fmt.Errorf("%s: %w", a.dest, err))
			continue
		}
		logf("%s pret (%.1f Mo, %s)", a.dest, float64(sizeOf(dest))/(1<<20), time.Since(start).Round(time.Millisecond))
	}

	// Le vocabulaire BERT des MiniLM est identique : le modele discover
	// reutilise celui du modele par defaut, comme documente dans le README.
	src := filepath.Join(root, "vocab.txt")
	dst := filepath.Join(root, "paraphrase-MiniLM-L3-v2", "vocab.txt")
	if fi, serr := os.Stat(dst); serr != nil || fi.Size() == 0 {
		if data, rerr := os.ReadFile(src); rerr == nil {
			if werr := writeFileAtomic(dst, data); werr != nil {
				errs = append(errs, werr)
			}
		}
	}
	return errors.Join(errs...)
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

func sizeOf(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// downloadFile copie une URL vers dest de facon atomique : ecriture d'un
// .part, controle de taille minimale, puis renommage.
func downloadFile(ctx context.Context, client *http.Client, a onnxAsset, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err //nolint:wrapcheck
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return err //nolint:wrapcheck
	}
	resp, err := client.Do(req)
	if err != nil {
		return err //nolint:wrapcheck
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err //nolint:wrapcheck
	}
	written, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err == nil && a.min > 0 && written < a.min {
		err = fmt.Errorf("taille %d < %d attendue", written, a.min)
	}
	if err != nil {
		os.Remove(tmp)
		return err //nolint:wrapcheck
	}
	return os.Rename(tmp, dest) //nolint:wrapcheck
}

// downloadArchiveMember telecharge une archive (tar.gz ou zip) et n'en extrait
// que le membre demande, directement vers dest.
func downloadArchiveMember(ctx context.Context, client *http.Client, a onnxAsset, dest, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err //nolint:wrapcheck
	}
	tmp, err := os.CreateTemp(dir, "ort-*.archive")
	if err != nil {
		return err //nolint:wrapcheck
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // sans effet apres reussite : la copie est faite

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		tmp.Close()
		return err //nolint:wrapcheck
	}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return err //nolint:wrapcheck
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err //nolint:wrapcheck
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return err //nolint:wrapcheck
	}

	extractErr := extractArchiveMember(tmp, strings.TrimSuffix(path.Base(a.url), ".tgz")+"|"+a.member, dest)
	if cerr := tmp.Close(); cerr != nil && extractErr == nil {
		extractErr = cerr
	}
	return extractErr
}

// extractArchiveMember choisit tar.gz ou zip d'apres l'extension de l'URL
// encodee dans spec ("<nom>|<membre>").
func extractArchiveMember(f *os.File, spec, dest string) error {
	archiveURL, member, _ := strings.Cut(spec, "|")
	var err error
	if strings.HasSuffix(archiveURL, ".zip") {
		err = extractZipMember(f, member, dest)
	} else {
		err = extractTarGzMember(f, member, dest)
	}
	if err != nil {
		os.Remove(dest + ".part")
	}
	return err
}

func extractTarGzMember(r io.Reader, member, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err //nolint:wrapcheck
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("membre %q introuvable dans l'archive", member)
		}
		if err != nil {
			return err //nolint:wrapcheck
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, member) {
			continue
		}
		return copyMemberToDest(tr, dest)
	}
}

func extractZipMember(f *os.File, member, dest string) error {
	fi, err := f.Stat()
	if err != nil {
		return err //nolint:wrapcheck
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return err //nolint:wrapcheck
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, member) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err //nolint:wrapcheck
		}
		defer rc.Close()
		return copyMemberToDest(rc, dest)
	}
	return fmt.Errorf("membre %q introuvable dans l'archive", member)
}

// copyMemberToDest ecrit le membre extrait de facon atomique (.part puis
// renommage), en verifiant la taille minimale.
func copyMemberToDest(r io.Reader, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err //nolint:wrapcheck
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err //nolint:wrapcheck
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err == nil && n < 1<<20 {
		err = fmt.Errorf("membre extrait trop petit (%d octets)", n)
	}
	if err != nil {
		return err //nolint:wrapcheck
	}
	return os.Rename(tmp, dest) //nolint:wrapcheck
}

// writeFileAtomic place un contenu deja charge de facon atomique.
func writeFileAtomic(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err //nolint:wrapcheck
	}
	tmp := dest + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err //nolint:wrapcheck
	}
	return os.Rename(tmp, dest) //nolint:wrapcheck
}
