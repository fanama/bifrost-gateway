package web

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"bridge-gateway/application"
)

func (s *ragController) uploadDocument(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxWebRAGUpload); err != nil {
		s.renderToast(w, "#rag-error", "formulaire multipart avec un champ 'file' requis")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.renderToast(w, "#rag-error", "formulaire multipart avec un champ 'file' requis")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxWebRAGUpload+1))
	if err != nil {
		s.renderToast(w, "#rag-error", "lecture du fichier impossible")
		return
	}
	if len(content) > maxWebRAGUpload {
		s.renderToast(w, "#rag-error", "fichier trop volumineux")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	doc, err := s.rag.Ingest(r.Context(), pid, header.Filename, header.Header.Get("Content-Type"), content, application.RAGIngestOptions{})
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	s.renderDocuments(w, r, pid, "Document « "+doc.FileName+" » indexé.")
}

func (s *ragController) documentChunks(w http.ResponseWriter, r *http.Request) {
	pid := strings.TrimSpace(r.URL.Query().Get("project_id"))
	doc, chunks, err := s.rag.DocumentChunks(r.Context(), pid, r.PathValue("id"))
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_document_chunks", map[string]any{"Document": doc, "Chunks": chunks})
}

func (s *ragController) deleteDocument(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#rag-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	if err := s.rag.DeleteDocument(r.Context(), pid, r.PathValue("id")); err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	s.renderDocuments(w, r, pid, "Document supprimé.")
}

func (s *ragController) renderDocuments(w http.ResponseWriter, r *http.Request, pid, message string) {
	docs, err := s.rag.Documents(r.Context(), pid)
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "rag_documents_body", map[string]any{"Documents": docs, "Message": message})
}

func (s *ragController) askQuestion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderToast(w, "#rag-error", "requête invalide")
		return
	}
	pid := strings.TrimSpace(r.FormValue("project_id"))
	question := strings.TrimSpace(r.FormValue("question"))
	opts := application.RAGQueryOptions{TopK: formInt(r.FormValue("top_k"))}
	if r.FormValue("mode") == "search" {
		result, err := s.rag.Search(r.Context(), pid, question, opts)
		if err != nil {
			s.renderToast(w, "#rag-error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.render(w, "rag_search", map[string]any{
			"Question": question,
			"Chunks":   result.Chunks,
			"Degraded": result.Degraded,
			"State":    result.IndexState,
		})
		return
	}
	answer, err := s.rag.Query(r.Context(), pid, question, opts)
	if err != nil {
		s.renderToast(w, "#rag-error", err.Error())
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
