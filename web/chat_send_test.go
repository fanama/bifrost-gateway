package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"bridge-gateway/application"
	"bridge-gateway/domain"
)

type stubChatService struct {
	history []domain.ChatMessage
	message string
	reply   string
	err     error
}

func (s *stubChatService) Send(
	_ context.Context,
	_ string,
	history []domain.ChatMessage,
	message string,
	_ *domain.RequestOptions,
) (application.ChatResult, error) {
	s.history = history
	s.message = message
	if s.err != nil {
		return application.ChatResult{}, s.err
	}
	return application.ChatResult{
		Messages: append(history,
			domain.ChatMessage{Role: "user", Content: message},
			domain.ChatMessage{Role: "assistant", Content: s.reply},
		),
		Model:          "mistral-large",
		RequestedModel: "mistral-large",
	}, nil
}

// stubChatConfigs ne implemente que ce que la discussion utilise : la liste et
// la lecture d'une configuration. Les autres methodes du port restent nil.
type stubChatConfigs struct {
	configService
}

func (stubChatConfigs) List(context.Context) ([]domain.ChatConfig, error) {
	return []domain.ChatConfig{{ID: "cfg-1", Name: "Test", Model: "mistral-large", ProjectID: "p1", Active: true}}, nil
}

func (stubChatConfigs) Get(context.Context, string) (*domain.ChatConfig, error) {
	return &domain.ChatConfig{ID: "cfg-1", Name: "Test", Model: "mistral-large", ProjectID: "p1", Active: true}, nil
}

func chatSendMux(t *testing.T, stub *stubChatService) *http.ServeMux {
	t.Helper()
	server, err := NewServer(stub, nil, nil, nil, nil, nil, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	return mux
}

func TestChatSendAppendsOnlyTheNewTurnWithMarkdown(t *testing.T) {
	stub := &stubChatService{reply: "## Réponse\n\n- point **un**"}
	mux := chatSendMux(t, stub)

	form := url.Values{}
	form.Set("config_id", "cfg-1")
	form.Set("message", "  Et ensuite ?  ")
	form.Set("history", `[{"role":"user","content":"Bonjour"},{"role":"assistant","content":"Salut"}]`)

	req := httptest.NewRequest("POST", "/chat/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", rec.Code)
	}

	// Les anciens messages sont transmis au modele a chaque tour.
	if len(stub.history) != 2 {
		t.Fatalf("history sent to the model = %d messages, want 2", len(stub.history))
	}
	if stub.message != "Et ensuite ?" {
		t.Fatalf("message sent = %q", stub.message)
	}

	body := rec.Body.String()

	// Seul le nouvel echange est rendu : l'ancien historique n'est pas rejoue,
	// donc le DOM de la discussion ne peut pas le perdre.
	if strings.Contains(body, "Bonjour") || strings.Contains(body, "Salut") {
		t.Fatalf("old messages must not be re-rendered, got:\n%s", body)
	}
	if strings.Count(body, `class="chat-message `) != 2 {
		t.Fatalf("expected exactly the user and assistant bubbles, got:\n%s", body)
	}
	if !strings.Contains(body, `data-content="## Réponse`) {
		t.Fatalf("assistant markdown source must be kept for the history, got:\n%s", body)
	}

	// Le markdown est rendu, pas affiche brut.
	for _, want := range []string{
		"<h2>Réponse</h2>",
		"<li>point <strong>un</strong></li>",
		`data-role="assistant"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	// Le markdown brut ne doit subsister que dans data-content (historique),
	// jamais dans le texte affiche.
	if n := strings.Count(body, "## Réponse"); n != 1 {
		t.Fatalf("raw markdown displayed %d times, want 1 (the data-content attribute):\n%s", n, body)
	}
}

func chatRAGMux(t *testing.T, stub *stubChatService, rag *stubRAG) *http.ServeMux {
	t.Helper()
	server, err := NewServer(stub, stubChatConfigs{}, nil, nil, nil, nil, nil, nil, nil, "", rag)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	return mux
}

func TestChatPageShowsRAGCheckbox(t *testing.T) {
	server, err := NewServer(nil, stubChatConfigs{}, nil, nil, nil, nil, nil, nil, nil, "", &stubRAG{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(rec.Body.String(), `name="use_rag"`) {
		t.Fatalf("RAG checkbox missing from the discussion page: %s", rec.Body.String())
	}
}

func TestChatSendUsesRAGWhenChecked(t *testing.T) {
	stub := &stubChatService{reply: "Orion [1]"}
	mux := chatRAGMux(t, stub, &stubRAG{})

	form := url.Values{}
	form.Set("config_id", "cfg-1")
	form.Set("message", "Qui est Orion ?")
	form.Set("use_rag", "1")

	req := httptest.NewRequest("POST", "/chat/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Le modele recoit le passage retrouve et la question.
	if !strings.Contains(stub.message, "Passage alpha") || !strings.Contains(stub.message, "Qui est Orion ?") {
		t.Fatalf("grounded prompt missing context: %q", stub.message)
	}

	body := rec.Body.String()
	// La bulle utilisateur garde ce que la personne a tape, pas le prompt.
	if !strings.Contains(body, `data-content="Qui est Orion ?"`) {
		t.Fatalf("user bubble must keep the typed message: %s", body)
	}
	if strings.Contains(body, "Passage alpha") {
		t.Fatalf("grounded prompt leaked into the conversation: %s", body)
	}
	// Les sources du corpus sont affichees avec la reponse.
	if !strings.Contains(body, "notes.txt") || !strings.Contains(body, `class="badge">RAG</span>`) {
		t.Fatalf("RAG sources missing from the reply: %s", body)
	}
}

func TestChatSendErrorKeepsConversationUntouched(t *testing.T) {
	stub := &stubChatService{err: errors.New("provider indisponible")}
	mux := chatSendMux(t, stub)

	form := url.Values{}
	form.Set("config_id", "cfg-1")
	form.Set("message", "Bonjour")

	req := httptest.NewRequest("POST", "/chat/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got := rec.Header().Get("HX-Reswap"); got != "innerHTML" {
		t.Fatalf("HX-Reswap = %q, want innerHTML", got)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#chat-error" {
		t.Fatalf("HX-Retarget = %q, want #chat-error", got)
	}
	if strings.Contains(rec.Body.String(), `class="chat-message `) {
		t.Fatalf("an error must not append a bubble, got:\n%s", rec.Body.String())
	}
}
