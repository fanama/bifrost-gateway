package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// pathsFor resout l'identifiant d'un modele vers ses deux artefacts. Le
// modele par defaut conserve les chemins plats resolus depuis
// l'environnement (ONNX_MODEL_PATH / ONNX_VOCAB_PATH) ; tout autre
// identifiant designe un repertoire de OnnxModelsDir, convention qui rend les
// modeles locaux decouvrables sans configuration.
func (e *OnnxEmbedder) pathsFor(modelID string) (modelPath, vocabPath string, err error) {
	id := strings.TrimSpace(modelID)
	if id == "" {
		id = domain.ModelOnnxMiniLM
	}
	if id == domain.ModelOnnxMiniLM {
		return e.opts.ModelPath, e.opts.VocabPath, nil
	}
	rel := strings.TrimPrefix(id, domain.OnnxModelPrefix)
	// Un identifiant ne designe qu'un repertoire direct : ni sous-chemin, ni
	// remontee d'arborescence — l'id vient d'une requete client.
	if rel == "" || rel == "." || rel == ".." ||
		strings.Contains(rel, "..") || strings.ContainsAny(rel, `/\\`) {
		return "", "", &domain.InvalidRequestError{
			Param:  "model",
			Reason: fmt.Sprintf("onnx model id invalid: %q (expected %s<repertoire> under %s/)", id, domain.OnnxModelPrefix, e.opts.ModelsDir),
		}
	}
	base := filepath.Join(e.opts.ModelsDir, rel)
	return filepath.Join(base, "model.onnx"), filepath.Join(base, "vocab.txt"), nil
}

// stateLocked retourne l'etat charge du modele, cree au premier appel. La
// bibliotheque et l'environnement ONNX sont partages par tous les modeles :
// seule la session et le tokenizer sont propres a chacun. Chaque manque est
// signale par un InvalidRequestError portant sur le parametre "model" : le
// testeur l'affiche en toast, l'API le rend en 400 — dans les deux cas le
// client voit exactement quel fichier manque plutot qu'une erreur d'infrence
// obscure.
func (e *OnnxEmbedder) stateLocked(modelID string) (*onnxModelState, error) {
	key := strings.TrimSpace(modelID)
	if key == "" {
		key = domain.ModelOnnxMiniLM
	}
	if state, ok := e.sessions[key]; ok {
		return state, nil
	}

	// L'identifiant est valide avant toute lecture disque : un id invalide
	// est reconnu memes sans bibliotheque ni modele installes.
	modelPath, vocabPath, err := e.pathsFor(key)
	if err != nil {
		return nil, err
	}

	if err := e.ensureRuntime(); err != nil {
		return nil, err
	}

	if err := e.checkModelFiles(key, modelPath, vocabPath); err != nil {
		return nil, err
	}
	state, err := e.openState(modelPath, vocabPath)
	if err != nil {
		return nil, err
	}
	if e.sessions == nil {
		e.sessions = make(map[string]*onnxModelState, 2)
	}
	e.sessions[key] = state
	return state, nil
}

// checkModelFiles reports exactly which artifact is missing, with a hint that
// depends on whether the built-in or a local model was requested.
func (e *OnnxEmbedder) checkModelFiles(key, modelPath, vocabPath string) error {
	hint := "set ONNX_MODEL_PATH / ONNX_VOCAB_PATH"
	if key != domain.ModelOnnxMiniLM {
		hint = fmt.Sprintf("a local model needs %s/<repertoire>/{model.onnx,vocab.txt}", e.opts.ModelsDir)
	}
	for _, missing := range []struct {
		label string
		path  string
	}{
		{"onnx model", modelPath},
		{"onnx vocab", vocabPath},
	} {
		if _, err := os.Stat(missing.path); err != nil {
			return &domain.InvalidRequestError{
				Param:  "model",
				Reason: fmt.Sprintf("onnx model not configured: %s not found at %q (%s)", missing.label, missing.path, hint),
			}
		}
	}
	return nil
}

// openState creates the per-model session and tokenizer.
func (e *OnnxEmbedder) openState(modelPath, vocabPath string) (*onnxModelState, error) {
	// CPUExecutionProvider is the default: naming CPU explicitly fails on some
	// ONNX Runtime builds ("Unknown provider name"), so the list stays empty.
	session, err := e.runtime.NewSession(e.env, modelPath, &onnxruntime.SessionOptions{})
	if err != nil {
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx session init failed: " + err.Error()}
	}
	tok, err := newWordPieceTokenizer(vocabPath, e.opts.MaxSeqLen)
	if err != nil {
		session.Close()
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx tokenizer init failed: " + err.Error()}
	}
	return &onnxModelState{session: session, tok: tok}, nil
}

// ensureRuntime loads the shared ONNX library and environment once.
func (e *OnnxEmbedder) ensureRuntime() error {
	if e.runtime != nil {
		return nil
	}
	if _, err := os.Stat(e.opts.LibraryPath); err != nil {
		return &domain.InvalidRequestError{
			Param:  "model",
			Reason: fmt.Sprintf("onnx runtime not configured: onnx runtime library not found at %q (set ONNX_RUNTIME_LIB)", e.opts.LibraryPath),
		}
	}
	rt, err := onnxruntime.NewRuntime(e.opts.LibraryPath, onnxAPIVersion)
	if err != nil {
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx runtime load failed: " + err.Error()}
	}
	env, err := rt.NewEnv("embedding", onnxruntime.LoggingLevelWarning)
	if err != nil {
		rt.Close()
		return &domain.InvalidRequestError{Param: "model", Reason: "onnx env init failed: " + err.Error()}
	}
	e.runtime, e.env = rt, env
	return nil
}
