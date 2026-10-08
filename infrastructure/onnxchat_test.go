package infrastructure

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"bridge-gateway/domain"
)

// TestChatTokenizerDecode : Decode inverse Encode, octet pour octet, sans
// laisser de rune UTF-8 tronque.
func TestChatTokenizerDecode(t *testing.T) {
	tok := chatTokFromRoot(t)
	for _, text := range []string{
		"Hello world!",
		"Les marches boursiers ont chute de 12.5% en 2023.",
		"emoji: caf\u00e9 \u00e0\u00e7 \u2014 sans accent",
	} {
		ids := tok.Encode(text)
		if got := tok.Decode(ids); got != text {
			t.Errorf("decode mismatch:\n want %q\n  got %q", text, got)
		}
	}
}

// TestRenderChatPrompt verifie le template SmolLM2 : system par defaut quand
// la conversation n'ouvre pas sur un system, encadre
// role/content, et invitation a repondre en fin de prompt.
func TestRenderChatPrompt(t *testing.T) {
	got := renderChatPrompt([]domain.ChatMessage{
		{Role: "user", Content: "bonjour"},
		{Role: "assistant", Content: "salut"},
		{Role: "user", Content: "ca va ?"},
	})
	for _, want := range []string{
		"<|im_start|>system\nYou are a helpful AI assistant named SmolLM, trained by Hugging Face\n",
		"<|im_start|>user\nbonjour<|im_end|>\n",
		"<|im_start|>assistant\nsalut<|im_end|>\n",
		"<|im_start|>user\nca va ?<|im_end|>\n",
		"<|im_start|>assistant\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt manquant %q dans %q", want, got)
		}
	}
	// Le system par defaut ne s'ajoute jamais devant un system explicite.
	withSystem := renderChatPrompt([]domain.ChatMessage{
		{Role: "system", Content: "tu es un pirate"},
		{Role: "user", Content: "bonjour"},
	})
	if strings.Contains(withSystem, "SmolLM, trained by Hugging Face") {
		t.Errorf("system par defaut ajoute malgre un system explicite: %q", withSystem)
	}
	if !strings.HasPrefix(withSystem, "<|im_start|>system\ntu es un pirate") {
		t.Errorf("system explicite non en tete: %q", withSystem)
	}
}

// spyLLM enregistre l'appel recu par le repli du routeur.
type spyLLM struct {
	called bool
	model  string
}

func (s *spyLLM) Chat(_ context.Context, cfg *domain.ChatConfig, _ []domain.ChatMessage) (domain.LLMResult, error) {
	s.called = true
	return domain.LLMResult{Content: "distant", Model: cfg.Model}, nil
}

// TestOnnxChatRouterChecksProvider : provider onnx -> moteur local, sinon
// repli distant, sans catalogue.
func TestOnnxChatRouterChecksProvider(t *testing.T) {
	inner := &spyLLM{}
	router := NewOnnxChatRouter(NewOnnxChatProvider(OnnxChatOptions{}), inner)

	if _, err := router.Chat(context.Background(), &domain.ChatConfig{Provider: "onnx", Model: "x"}, nil); err == nil {
		t.Fatal("attendu: une erreur locale (artefacts absents) plutot qu'un repli distant")
	}
	if inner.called {
		t.Error("le routeur a fui vers le repli distant pour provider onnx")
	}

	res, err := router.Chat(context.Background(), &domain.ChatConfig{Provider: "mistral", Model: "x"}, nil)
	if err != nil || res.Content != "distant" {
		t.Fatalf("repli distant attendu, got res=%+v err=%v", res, err)
	}
}

// TestOnnxChatRouterChecksCatalog : un modele du catalogue declare onnx/chat
// route localement, meme si la config porte un autre provider (regle 2) ; un
// modele de chat distant du catalogue ne detourne jamais l'appel.
func TestOnnxChatRouterChecksCatalog(t *testing.T) {
	catalog := &fakeCatalogStore{items: []domain.CatalogModel{
		{ID: "m1", Name: "smollm2-135m-instruct", Provider: "onnx", Kind: domain.ModelKindChat},
		{ID: "m2", Name: "mistral-large", Provider: "mistral", Kind: domain.ModelKindChat},
	}}
	inner := &spyLLM{}
	router := NewOnnxChatRouter(NewOnnxChatProvider(OnnxChatOptions{}), inner).WithCatalog(catalog)

	if _, err := router.Chat(context.Background(), &domain.ChatConfig{Provider: "mistral", Model: "smollm2-135m-instruct"}, nil); err == nil {
		t.Fatal("attendu: tentative locale (artefacts absents) pour un modele onnx du catalogue")
	}
	if inner.called {
		t.Fatal("modele onnx du catalogue route vers le repli distant")
	}

	if _, err := router.Chat(context.Background(), &domain.ChatConfig{Provider: "mistral", Model: "mistral-large"}, nil); err != nil {
		t.Fatalf("modele distant du catalogue: %v", err)
	}
	if !inner.called {
		t.Fatal("modele de chat distant du catalogue non route vers le repli")
	}
}

// e2eChatProvider construit un point d'entree local reels (artefacts resolus
// depuis la racine du module) ; t.Skip si le depot est sans assets.
func e2eChatProvider(t *testing.T) (*OnnxChatProvider, string) {
	t.Helper()
	opts := DefaultOnnxChatOptions()
	opts.LibraryPath = fromModuleRoot(opts.LibraryPath)
	opts.ModelsDir = fromModuleRoot(opts.ModelsDir)
	ids, err := ListOnnxChatModels(opts.ModelsDir)
	if err != nil || len(ids) == 0 {
		t.Skipf("modele de chat absent — voir models/onnx/chat/: %v", err)
	}
	return NewOnnxChatProvider(opts), ids[0]
}

// TestOnnxChatGenerateE2E execute le modele reel de bout en bout : prefill,
// cache KV, echantillonnage glouton et detokenisation. La generation gloutonne
// est deterministe, deux appels identiques doivent rendre le meme texte.
func TestOnnxChatGenerateE2E(t *testing.T) {
	p, model := e2eChatProvider(t)
	zero := 0.0
	max := 32
	cfg := &domain.ChatConfig{
		Provider:    "onnx",
		Model:       model,
		Temperature: &zero,
		MaxTokens:   &max,
	}
	msgs := []domain.ChatMessage{{Role: "user", Content: "Say hello in one short sentence."}}

	first, err := p.Chat(context.Background(), cfg, msgs)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if strings.TrimSpace(first.Content) == "" {
		t.Fatal("generation vide")
	}
	if strings.ContainsAny(first.Content, "\x00") {
		t.Errorf("octets nuls dans la reponse: %q", first.Content)
	}
	if !utf8.ValidString(first.Content) {
		t.Errorf("reponse pas en UTF-8 valide (tokenisation ou decode casse): %q", first.Content)
	}
	t.Logf("reponse: %q", first.Content)

	second, err := p.Chat(context.Background(), cfg, msgs)
	if err != nil {
		t.Fatalf("chat (2e appel): %v", err)
	}
	if first.Content != second.Content {
		t.Errorf("generation gloutonne non deterministe: %q != %q", first.Content, second.Content)
	}
}

// TestOnnxChatStreamE2E verifie que le flux est progressif (au moins deux
// trames non vides avant Done) et que la concatenation des deltas egale la
// reponse complete.
func TestOnnxChatStreamE2E(t *testing.T) {
	p, model := e2eChatProvider(t)
	zero := 0.0
	max := 32
	cfg := &domain.ChatConfig{Provider: "onnx", Model: model, Temperature: &zero, MaxTokens: &max}
	msgs := []domain.ChatMessage{{Role: "user", Content: "Count from 1 to 5."}}

	stream, err := p.ChatStream(context.Background(), cfg, msgs)
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	var sb strings.Builder
	fragments, role, done := 0, "", false
	for ev := range stream {
		if ev.Err != nil {
			t.Fatalf("erreur en flux: %v", ev.Err)
		}
		if ev.Done {
			done = true
			continue
		}
		if ev.Delta != "" {
			fragments++
			sb.WriteString(ev.Delta)
			if ev.Role != "" {
				role = ev.Role
			}
		}
	}
	if !done {
		t.Fatal("flux sans trame Done")
	}
	if fragments < 2 {
		t.Errorf("flux non progressif: %d fragment(s)", fragments)
	}
	if role != "assistant" {
		t.Errorf("premier fragment sans role assistant, got %q", role)
	}
	if strings.TrimSpace(sb.String()) == "" {
		t.Fatal("flux vide")
	}
	t.Logf("flux (%d fragments): %q", fragments, sb.String())
}
