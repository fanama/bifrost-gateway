package infrastructure

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListOnnxModels decouvre les modeles ONNX locaux : chaque repertoire de dir
// contenant a la fois model.onnx et vocab.txt est un modele utilisable sous
// l'id "onnx/<repertoire>". Les artefacts plats du modele par defaut
// (dir/model.onnx a la racine) ne sont pas un repertoire : ils restent couverts
// par le choix curate onnx/all-MiniLM-L6-v2.
//
// Une erreur d'enumeration (repertoire absent, lecture refusee...) est
// remontee telle quelle : c'est a l'appelant de trancher entre "aucun modele"
// et une vraie panne, pas au decouvert de l'avaler silencieusement.
func ListOnnxModels(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		base := filepath.Join(dir, name)
		if !isOnnxArtifact(base, "model.onnx") || !isOnnxArtifact(base, "vocab.txt") {
			continue
		}
		ids = append(ids, name)
	}
	sort.Strings(ids)
	return ids, nil
}

// isOnnxArtifact verifie qu'un fichier existe et n'est pas un repertoire :
// un chemin de fichier qui pointe sur un dossier ne doit pas valider le decouvert.
func isOnnxArtifact(base, name string) bool {
	info, err := os.Stat(filepath.Join(base, name))
	return err == nil && !info.IsDir()
}
