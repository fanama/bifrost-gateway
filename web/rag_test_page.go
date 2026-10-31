package web

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"bridge-gateway/application"
)

func (s *ragController) pageTest(w http.ResponseWriter, r *http.Request) {
	projects, pid, err := s.selection(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		s.renderToast(w, "#rag-test-error", err.Error())
		return
	}
	data := map[string]any{
		"Active":            "rag-test",
		"Title":             "RAG — Test",
		"Projects":          projects,
		"SelectedProjectID": pid,
	}
	if pid != "" {
		if data["Settings"], err = s.rag.Settings(r.Context(), pid); err != nil {
			s.renderToast(w, "#rag-test-error", err.Error())
			return
		}
		if data["Documents"], err = s.rag.Documents(r.Context(), pid); err != nil {
			s.renderToast(w, "#rag-test-error", err.Error())
			return
		}
	}
	s.render(w, "page_rag_test", data)
}

func (s *ragController) uploadDocument(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxWebRAGUpload); err != nil {
		s.renderToast(w, "#rag-test-error", "formulaire multipart avec un champ 'file' requis")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.renderToast(w, "#rag-test-error", "formulaire multipart avec un champ 'file' requis")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxWebRAGUpload+1))
	if err != nil {
		s.renderToast(w, "#rag-test-error", "lecture du fichier impossible")
		return
	}
	if len(content) > maxWebRAGUpload {
		s.renderToast(w, "#rag-test-error", "fichier trop volumineux")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	doc, err := s.rag.Ingest(r.Context(), pid, header.Filename, header.Header.Get("Content-Type"), content, application.RAGIngestOptions{})
	if err != nil {
		s.renderToast(w, "#rag-test-error", err.Error())
		return
	}
	s.renderDocuments(w, r, pid, "Document « "+doc.FileName+" » indexé.")
}

func (s *ragController) deleteDocument(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#rag-test-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	if err := s.rag.DeleteDocument(r.Context(), pid, r.PathValue("id")); err != nil {
		s.renderToast(w, "#rag-test-error", err.Error())
		return
	}
	s.renderDocuments(w, r, pid, "Document supprimé.")
}

func (s *ragController) renderDocuments(w http.ResponseWriter, r *http.Request, pid, message string) {
	docs, err := s.rag.Documents(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#rag-test-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_documents_body", map[string]any{"Documents": docs, "Message": message})
}

func (s *ragController) askQuestion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#rag-test-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	question := strings.TrimSpace(r.FormValue("question"))
	opts := application.RAGQueryOptions{TopK: formInt(r.FormValue("top_k"))}
	if r.FormValue("mode") == "search" {
		chunks, err := s.rag.Search(r.Context(), pid, question, opts)
		if err != nil {
			s.renderToast(w, "#rag-test-error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.render(w, "rag_search", map[string]any{"Question": question, "Chunks": chunks})
		return
	}
	answer, err := s.rag.Query(r.Context(), pid, question, opts)
	if err != nil {
		s.renderToast(w, "#rag-test-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_answer", map[string]any{"Question": question, "Result": answer, "Sources": answer.Sources})
}

func formInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
