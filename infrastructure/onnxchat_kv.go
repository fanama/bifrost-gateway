package infrastructure

import (
	"context"
	"fmt"

	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// onnxKVCache est le cache de sequence du modele : cle et valeur de chaque
// couche, en float32, mise a jour apres chaque pas (prefill puis decode).
type onnxKVCache struct {
	keys   [][]float32 // [couche] -> [1, kvHeads, len, headDim]
	values [][]float32
	length int
}

func newOnnxKVCache(layers, kvHeads, headDim int) *onnxKVCache {
	return &onnxKVCache{
		keys:   make([][]float32, layers),
		values: make([][]float32, layers),
	}
}

// step execute le modele sur les tokens donnes (prefill si le cache est vide,
// sinon decode d'un seul token), met a jour le cache et retourne les logits de
// la derniere position.
func (p *OnnxChatProvider) step(
	ctx context.Context,
	st *onnxChatModel,
	kv *onnxKVCache,
	ids []int64,
) ([]float32, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("onnx chat: empty step")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seq := len(ids)
	total := kv.length + seq
	layers := len(kv.keys)

	inputs := map[string]*onnxruntime.Value{}
	closeInputs := func() {
		for _, v := range inputs {
			if v != nil {
				v.Close()
			}
		}
	}
	var err error
	put := func(name string, data []int64, dims []int64) error {
		v, e := onnxruntime.NewTensorValue[int64](p.runtime, data, dims)
		if e != nil {
			return fmt.Errorf("onnx chat %s: %w", name, e)
		}
		inputs[name] = v
		return nil
	}
	if err = put("input_ids", ids, []int64{1, int64(seq)}); err != nil {
		closeInputs()
		return nil, err
	}
	mask := make([]int64, total)
	pos := make([]int64, seq)
	for i := range mask {
		mask[i] = 1
	}
	for i := range pos {
		pos[i] = int64(kv.length + i)
	}
	if err = put("attention_mask", mask, []int64{1, int64(total)}); err != nil {
		closeInputs()
		return nil, err
	}
	if err = put("position_ids", pos, []int64{1, int64(seq)}); err != nil {
		closeInputs()
		return nil, err
	}

	// Past de longueur nulle : purego refuse un buffer vide, on fournit un
	// element factice dont la shape annonce zero element (le graphe est
	// dynamique, il l'accepte).
	for l := 0; l < layers; l++ {
		for _, part := range []struct {
			name string
			data []float32
		}{{fmt.Sprintf("past_key_values.%d.key", l), kv.keys[l]},
			{fmt.Sprintf("past_key_values.%d.value", l), kv.values[l]}} {
			shape := []int64{1, int64(st.kvHeads), int64(kv.length), int64(st.headDim)}
			data := part.data
			if len(data) == 0 {
				data = []float32{0}
			}
			v, e := onnxruntime.NewTensorValue[float32](p.runtime, data, shape)
			if e != nil {
				closeInputs()
				return nil, fmt.Errorf("onnx chat %s: %w", part.name, e)
			}
			inputs[part.name] = v
		}
	}
	defer closeInputs()

	outputs, err := st.session.Run(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("onnx chat inference: %w", err)
	}
	defer func() {
		for _, v := range outputs {
			if v != nil {
				v.Close()
			}
		}
	}()

	logitVal, ok := outputs["logits"]
	if !ok || logitVal == nil {
		return nil, fmt.Errorf("onnx chat model exposes no logits output")
	}
	logits, shape, err := onnxruntime.GetTensorData[float32](logitVal)
	if err != nil {
		return nil, fmt.Errorf("onnx chat logits: %w", err)
	}
	want := int64(seq) * int64(vocabSize(shape))
	if len(logits) < int(want) {
		return nil, fmt.Errorf("onnx chat logits size mismatch: %d < %d", len(logits), want)
	}
	last := make([]float32, vocabSize(shape))
	copy(last, logits[len(logits)-len(last):])

	for l := 0; l < layers; l++ {
		for _, part := range []struct {
			name string
			dst  *[]float32
		}{{fmt.Sprintf("present.%d.key", l), &kv.keys[l]},
			{fmt.Sprintf("present.%d.value", l), &kv.values[l]}} {
			v := outputs[part.name]
			if v == nil {
				return nil, fmt.Errorf("onnx chat model missing output %s", part.name)
			}
			data, _, e := onnxruntime.GetTensorData[float32](v)
			if e != nil {
				return nil, fmt.Errorf("onnx chat %s: %w", part.name, e)
			}
			// present contient deja le passe + le nouveau : on remplace le
			// cache, on n'ajoute pas (sinon les lignes du passe seraient
			// comptees deux fois et la stride suivante serait fausse).
			*part.dst = append((*part.dst)[:0], data...)
		}
	}
	kv.length = total
	return last, nil
}

// vocabSize lit la derniere dimension des logits ([1, seq, vocab]).
func vocabSize(shape []int64) int {
	if len(shape) == 0 {
		return 0
	}
	return int(shape[len(shape)-1])
}
