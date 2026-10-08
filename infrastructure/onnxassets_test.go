package infrastructure

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAssets sert un manifeste factice depuis un serveur local et compte les
// requetes pour verifier ce qui est (re)touche.
func fakeAssets(t *testing.T, body []byte, hits *int, code int) (*httptest.Server, []onnxAsset) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, []onnxAsset{
		{dest: "model.onnx", url: srv.URL + "/model.onnx", min: int64(len(body))},
		{dest: "sub/vocab.txt", url: srv.URL + "/vocab.txt", min: int64(len(body))},
	}
}

// TestEnsureOnnxAssetsDownloadsMissing : les fichiers absents sont telecharges
// a leur destination (sous-repertoire compris), sans .part residuel.
func TestEnsureOnnxAssetsDownloadsMissing(t *testing.T) {
	hits := 0
	srv, assets := fakeAssets(t, []byte("contenu-artefact"), &hits, http.StatusOK)
	root := t.TempDir()

	var log bytes.Buffer
	if err := ensureOnnxAssets(context.Background(), root, assets, srv.Client(), &log); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, a := range assets {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.dest)))
		if err != nil {
			t.Fatalf("%s: %v", a.dest, err)
		}
		if string(got) != "contenu-artefact" {
			t.Errorf("%s: contenu %q", a.dest, got)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(a.dest)+".part")); !os.IsNotExist(err) {
			t.Errorf("%s: .part residuel", a.dest)
		}
	}
	if hits != len(assets) {
		t.Errorf("hits=%d, attendu %d", hits, len(assets))
	}
	// Second passage : tout est present, aucune requete.
	if err := ensureOnnxAssets(context.Background(), root, assets, srv.Client(), &log); err != nil {
		t.Fatalf("ensure (2e): %v", err)
	}
	if hits != len(assets) {
		t.Errorf("le 2e passage a retelecharge (hits=%d)", hits)
	}
}

// TestEnsureOnnxAssetsReplacesTruncated : un fichier plus petit que la taille
// minimale est un reste de telechargement coupe — il est repris.
func TestEnsureOnnxAssetsReplacesTruncated(t *testing.T) {
	hits := 0
	srv, assets := fakeAssets(t, []byte("contenu-artefact"), &hits, http.StatusOK)
	root := t.TempDir()
	truncated := filepath.Join(root, "model.onnx")
	if err := os.WriteFile(truncated, []byte("cour"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureOnnxAssets(context.Background(), root, assets, srv.Client(), io.Discard); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got, err := os.ReadFile(truncated)
	if err != nil || string(got) != "contenu-artefact" {
		t.Errorf("fichier tronque non remplace: %q err=%v", got, err)
	}
}

// TestEnsureOnnxAssetsHTTPError : une erreur reseau laisse une erreur
// journalisee et surtout aucun fichier final corrompu.
func TestEnsureOnnxAssetsHTTPError(t *testing.T) {
	hits := 0
	srv, assets := fakeAssets(t, nil, &hits, http.StatusNotFound)
	root := t.TempDir()

	err := ensureOnnxAssets(context.Background(), root, assets, srv.Client(), io.Discard)
	if err == nil {
		t.Fatal("attendu: une erreur pour un HTTP 404")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("erreur sans cause: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "model.onnx")); !os.IsNotExist(err) {
		t.Error("fichier cree malgre le 404")
	}
}

// TestOnnxAutoDownloadGate : active par defaut, desactivable par variable.
func TestOnnxAutoDownloadGate(t *testing.T) {
	t.Setenv(onnxAutoDownloadEnv, "")
	if !onnxAutoDownloadEnabled() {
		t.Error("actif par defaut attendu")
	}
	for _, off := range []string{"0", "false", "OFF", " no "} {
		t.Setenv(onnxAutoDownloadEnv, off)
		if onnxAutoDownloadEnabled() {
			t.Errorf("%q doit desactiver", off)
		}
	}
}

// TestDefaultOnnxAssetsManifest : chemins relatifs propres, sources https, et
// bibliotheque resolue pour la plateforme courante.
func TestDefaultOnnxAssetsManifest(t *testing.T) {
	assets := defaultOnnxAssets()
	if len(assets) < 8 {
		t.Fatalf("manifeste incomplet: %d entrees", len(assets))
	}
	sawLib := false
	for _, a := range assets {
		if filepath.IsAbs(a.dest) || strings.Contains(a.dest, "..") {
			t.Errorf("chemin non sur: %q", a.dest)
		}
		if !strings.HasPrefix(a.url, "https://") {
			t.Errorf("source non https: %q", a.url)
		}
		if a.member != "" {
			sawLib = true
		}
	}
	if !sawLib {
		t.Error("bibliotheque ONNX absente du manifeste")
	}
}

// TestExtractArchiveMember : seul le membre demande est extrait, de facon
// atomique, depuis une archive tar.gz comme depuis un zip.
func TestExtractArchiveMember(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1<<20+16) // au moins 1 Mo exigé
	dir := t.TempDir()

	// tar.gz
	tg := filepath.Join(dir, "a.tgz")
	f, err := os.Create(tg)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pkg-1.0/lib/libonnx.so", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	f, _ = os.Open(tg)
	dest := filepath.Join(dir, "lib", "libonnx.so")
	if err := extractTarGzMember(f, "lib/libonnx.so", dest); err != nil {
		t.Fatalf("tar.gz: %v", err)
	}
	f.Close()
	assertFileContent(t, dest, payload)

	// zip
	zp := filepath.Join(dir, "a.zip")
	zf, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("onnxruntime-win-x64-1.23.0/lib/onnxruntime.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zf.Close()

	zf, _ = os.Open(zp)
	destZip := filepath.Join(dir, "lib", "onnxruntime.dll")
	if err := extractZipMember(zf, "lib/onnxruntime.dll", destZip); err != nil {
		t.Fatalf("zip: %v", err)
	}
	zf.Close()
	assertFileContent(t, destZip, payload)

	// Membre absent : erreur explicite, rien d'ecrit.
	zf, _ = os.Open(zp)
	defer zf.Close()
	if err := extractZipMember(zf, "lib/absent.dll", filepath.Join(dir, "absent.dll")); err == nil {
		t.Error("membre absent attendu en erreur")
	}
	if _, err := os.Stat(filepath.Join(dir, "absent.dll")); !os.IsNotExist(err) {
		t.Error("fichier cree pour un membre absent")
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: contenu different (%d vs %d octets)", path, len(got), len(want))
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Errorf("%s: .part residuel", path)
	}
}
