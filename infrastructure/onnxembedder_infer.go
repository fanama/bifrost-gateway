package infrastructure

import (
	"context"
	"fmt"
	"math"
	"strings"

	"bridge-gateway/domain"
	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// runLocked tokenise le lot, execute le modele designe par state et pool les
// sorties en un vecteur L2-normalise par texte.
func (e *OnnxEmbedder) runLocked(ctx context.Context, texts []string, state *onnxModelState) ([][]float32, error) {
	batch := len(texts)
	seq := state.tok.maxSeqLen

	ids := make([]int64, batch*seq)
	mask := make([]int64, batch*seq)
	types := make([]int64, batch*seq)
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tIDs, tMask, tTypes := state.tok.encode(text)
		copy(ids[i*seq:], tIDs)
		copy(mask[i*seq:], tMask)
		copy(types[i*seq:], tTypes)
	}

	// Seuls les inputs reels du modele sont fournis : passer token_type_ids
	// a un modele qui ne les declare pas fait echouer toute l'inference.
	available := make(map[string]bool, 3)
	for _, name := range state.session.InputNames() {
		available[name] = true
	}
	if !available["input_ids"] {
		return nil, &domain.InvalidRequestError{Param: "model", Reason: "onnx model has no input_ids input; unsupported architecture"}
	}

	inputs := map[string]*onnxruntime.Value{}
	closeInputs := func() {
		for _, v := range inputs {
			v.Close()
		}
	}
	var err error
	if inputs["input_ids"], err = onnxruntime.NewTensorValue[int64](e.runtime, ids, []int64{int64(batch), int64(seq)}); err != nil {
		return nil, fmt.Errorf("onnx input_ids: %w", err)
	}
	if available["attention_mask"] {
		if inputs["attention_mask"], err = onnxruntime.NewTensorValue[int64](e.runtime, mask, []int64{int64(batch), int64(seq)}); err != nil {
			closeInputs()
			return nil, fmt.Errorf("onnx attention_mask: %w", err)
		}
	}
	if available["token_type_ids"] {
		if inputs["token_type_ids"], err = onnxruntime.NewTensorValue[int64](e.runtime, types, []int64{int64(batch), int64(seq)}); err != nil {
			closeInputs()
			return nil, fmt.Errorf("onnx token_type_ids: %w", err)
		}
	}
	defer closeInputs()

	outputs, err := state.session.Run(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("onnx inference: %w", err)
	}
	defer func() {
		for _, v := range outputs {
			if v != nil {
				v.Close()
			}
		}
	}()

	value, name, err := pickEmbeddingOutput(state.session, outputs)
	if err != nil {
		return nil, err
	}
	data, shape, err := onnxruntime.GetTensorData[float32](value)
	if err != nil {
		return nil, fmt.Errorf("onnx read %s: %w", name, err)
	}

	vectors, err := poolOutput(data, shape, mask, batch, seq)
	if err != nil {
		return nil, err
	}
	for _, vec := range vectors {
		l2Normalize(vec)
	}
	if state.dims == 0 && len(vectors) > 0 {
		state.dims = len(vectors[0])
	}
	return vectors, nil
}

// pickEmbeddingOutput choisit la sortie a pooler, par preference
// sentence_embedding (export optimum, deja pool) puis last_hidden_state
// (export brut, a moyenner), enfin la premiere sortie declaree par le
// modele — l'ordre des sorties est une information du modele, pas un
// iteration de map.
func pickEmbeddingOutput(session *onnxruntime.Session, outputs map[string]*onnxruntime.Value) (*onnxruntime.Value, string, error) {
	for _, want := range []string{"sentence_embedding", "last_hidden_state"} {
		if v, ok := outputs[want]; ok && v != nil {
			return v, want, nil
		}
	}
	for _, name := range session.OutputNames() {
		if v, ok := outputs[name]; ok && v != nil {
			return v, name, nil
		}
	}
	return nil, "", fmt.Errorf("onnx model exposes no usable output (have: %s)", strings.Join(session.OutputNames(), ", "))
}

// poolOutput convertit la sortie du modele en un vecteur par texte :
// [batch, dims] est deja pool (sentence_embedding), [batch, seq, dims] est
// moyenne sur les positions reelles (attention mask), les autres formes sont
// refusees plutot que devinees.
func poolOutput(data []float32, shape, mask []int64, batch, seq int) ([][]float32, error) {
	switch len(shape) {
	case 2:
		if int(shape[0]) != batch {
			return nil, fmt.Errorf("onnx output batch mismatch: %d != %d", shape[0], batch)
		}
		dims := int(shape[1])
		if len(data) != batch*dims {
			return nil, fmt.Errorf("onnx output size mismatch: %d != %d", len(data), batch*dims)
		}
		out := make([][]float32, batch)
		for i := range out {
			vec := make([]float32, dims)
			copy(vec, data[i*dims:(i+1)*dims])
			out[i] = vec
		}
		return out, nil
	case 3:
		if int(shape[0]) != batch || int(shape[1]) != seq {
			return nil, fmt.Errorf("onnx output shape mismatch: %v (batch %d, seq %d)", shape, batch, seq)
		}
		dims := int(shape[2])
		if len(data) != batch*seq*dims {
			return nil, fmt.Errorf("onnx output size mismatch: %d != %d", len(data), batch*seq*dims)
		}
		out := make([][]float32, batch)
		for i := 0; i < batch; i++ {
			vec := make([]float32, dims)
			count := int64(0)
			for s := 0; s < seq; s++ {
				if mask[i*seq+s] == 0 {
					continue
				}
				count++
				base := (i*seq + s) * dims
				for d := 0; d < dims; d++ {
					vec[d] += data[base+d]
				}
			}
			if count > 0 {
				inv := float32(1) / float32(count)
				for d := range vec {
					vec[d] *= inv
				}
			}
			out[i] = vec
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported onnx output rank %d (shape %v)", len(shape), shape)
	}
}

// l2Normalize ramene un vecteur sur la sphere unite : la similarite cosinus
// du testeur et les distances des clients deviennent un simple produit scalaire.
func l2Normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= inv
	}
}
