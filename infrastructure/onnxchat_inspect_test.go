package infrastructure

import (
	"fmt"
	"testing"

	"github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// TestOnnxChatInspect — inspection temporaire des entrees/sorties du graphe
// du modele de chat, pour concevoir la boucle KV-cache.
func TestOnnxChatInspect(t *testing.T) {
	opts := DefaultOnnxEmbedderOptions()
	lib := fromModuleRoot(opts.LibraryPath)
	model := fromModuleRoot("models/onnx/chat/smollm2-135m-instruct/model.onnx")

	rt, err := onnxruntime.NewRuntime(lib, onnxAPIVersion)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	defer rt.Close()
	env, err := rt.NewEnv("inspect", onnxruntime.LoggingLevelWarning)
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	defer env.Close()
	sess, err := rt.NewSession(env, model, &onnxruntime.SessionOptions{})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer sess.Close()

	fmt.Println("INPUTS:")
	for _, n := range sess.InputNames() {
		fmt.Println("  -", n)
	}
	fmt.Println("OUTPUTS:")
	for _, n := range sess.OutputNames() {
		fmt.Println("  -", n)
	}
}
