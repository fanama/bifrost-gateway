package infrastructure

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Ce fichier contient le transport des artefacts ONNX : telechargement des
// fichiers simples et extraction d'un membre d'archive, toujours de facon
// atomique (.part puis renommage). Le manifeste et l'orchestration vivent dans
// onnxassets.go.

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

	if err := fetchURLToFile(ctx, client, a.url, tmp); err != nil {
		tmp.Close()
		return err
	}
	extractErr := extractArchiveMember(tmp, strings.TrimSuffix(path.Base(a.url), ".tgz")+"|"+a.member, dest)
	if cerr := tmp.Close(); cerr != nil && extractErr == nil {
		extractErr = cerr
	}
	return extractErr
}

// fetchURLToFile streams url into dst and rewinds it for extraction.
func fetchURLToFile(ctx context.Context, client *http.Client, url string, dst *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return err //nolint:wrapcheck
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return err //nolint:wrapcheck
	}
	return nil
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
