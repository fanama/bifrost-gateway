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

// step executes the model on the given tokens (prefill when the cache is
// empty, otherwise a one-token decode), updates the cache, and returns the
// logits of the last position.
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

	inputs, err := p.stepInputs(st, kv, ids, seq, total)
	if err != nil {
		return nil, err
	}
	defer closeValues(inputs)

	outputs, err := st.session.Run(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("onnx chat inference: %w", err)
	}
	defer closeValues(outputs)

	last, err := lastLogits(outputs, seq)
	if err != nil {
		return nil, err
	}
	if err := updateKVCache(kv, outputs, layers); err != nil {
		return nil, err
	}
	kv.length = total
	return last, nil
}

func closeValues(values map[string]*onnxruntime.Value) {
	for _, v := range values {
		if v != nil {
			v.Close()
		}
	}
}

// stepInputs builds the int tensors plus the past key/value tensors of every
// layer. A partially built map is closed before returning an error.
func (p *OnnxChatProvider) stepInputs(st *onnxChatModel, kv *onnxKVCache, ids []int64, seq, total int) (map[string]*onnxruntime.Value, error) {
	inputs := map[string]*onnxruntime.Value{}
	if err := p.baseInputs(inputs, ids, seq, total, kv.length); err != nil {
		closeValues(inputs)
		return nil, err
	}
	if err := p.pastInputs(inputs, st, kv); err != nil {
		closeValues(inputs)
		return nil, err
	}
	return inputs, nil
}

func (p *OnnxChatProvider) baseInputs(inputs map[string]*onnxruntime.Value, ids []int64, seq, total, past int) error {
	mask := make([]int64, total)
	pos := make([]int64, seq)
	for i := range mask {
		mask[i] = 1
	}
	for i := range pos {
		pos[i] = int64(past + i)
	}
	tensors := []struct {
		name string
		data []int64
		dims []int64
	}{
		{"input_ids", ids, []int64{1, int64(seq)}},
		{"attention_mask", mask, []int64{1, int64(total)}},
		{"position_ids", pos, []int64{1, int64(seq)}},
	}
	for _, t := range tensors {
		v, err := onnxruntime.NewTensorValue[int64](p.runtime, t.data, t.dims)
		if err != nil {
			return fmt.Errorf("onnx chat %s: %w", t.name, err)
		}
		inputs[t.name] = v
	}
	return nil
}

// pastInputs feeds the cache of each layer. An empty past buffer is rejected
// by purego, so a dummy element is passed with a shape announcing zero items;
// the graph is dynamic and accepts it.
func (p *OnnxChatProvider) pastInputs(inputs map[string]*onnxruntime.Value, st *onnxChatModel, kv *onnxKVCache) error {
	shape := []int64{1, int64(st.kvHeads), int64(kv.length), int64(st.headDim)}
	for l := range kv.keys {
		parts := []struct {
			name string
			data []float32
		}{
			{fmt.Sprintf("past_key_values.%d.key", l), kv.keys[l]},
			{fmt.Sprintf("past_key_values.%d.value", l), kv.values[l]},
		}
		for _, part := range parts {
			data := part.data
			if len(data) == 0 {
				data = []float32{0}
			}
			v, err := onnxruntime.NewTensorValue[float32](p.runtime, data, shape)
			if err != nil {
				return fmt.Errorf("onnx chat %s: %w", part.name, err)
			}
			inputs[part.name] = v
		}
	}
	return nil
}

// lastLogits reads the final position's logits, shaped [1, seq, vocab].
func lastLogits(outputs map[string]*onnxruntime.Value, seq int) ([]float32, error) {
	value, ok := outputs["logits"]
	if !ok || value == nil {
		return nil, fmt.Errorf("onnx chat model exposes no logits output")
	}
	logits, shape, err := onnxruntime.GetTensorData[float32](value)
	if err != nil {
		return nil, fmt.Errorf("onnx chat logits: %w", err)
	}
	vocab := vocabSize(shape)
	want := seq * vocab
	if len(logits) < want {
		return nil, fmt.Errorf("onnx chat logits size mismatch: %d < %d", len(logits), want)
	}
	last := make([]float32, vocab)
	copy(last, logits[len(logits)-vocab:])
	return last, nil
}

// updateKVCache replaces each layer's cache with the present tensors. present
// already contains past + new, so replacing (not appending) avoids counting
// past rows twice.
func updateKVCache(kv *onnxKVCache, outputs map[string]*onnxruntime.Value, layers int) error {
	for l := 0; l < layers; l++ {
		parts := []struct {
			name string
			dst  *[]float32
		}{
			{fmt.Sprintf("present.%d.key", l), &kv.keys[l]},
			{fmt.Sprintf("present.%d.value", l), &kv.values[l]},
		}
		for _, part := range parts {
			value := outputs[part.name]
			if value == nil {
				return fmt.Errorf("onnx chat model missing output %s", part.name)
			}
			data, _, err := onnxruntime.GetTensorData[float32](value)
			if err != nil {
				return fmt.Errorf("onnx chat %s: %w", part.name, err)
			}
			*part.dst = append((*part.dst)[:0], data...)
		}
	}
	return nil
}

// vocabSize lit la derniere dimension des logits ([1, seq, vocab]).
func vocabSize(shape []int64) int {
	if len(shape) == 0 {
		return 0
	}
	return int(shape[len(shape)-1])
}
