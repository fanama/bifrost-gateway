package rag

import (
	"context"
	"path"
	"strconv"
	"strings"

	"bridge-gateway/domain"
	domainrag "bridge-gateway/domain/rag"
)

// Ingest validates, extracts, chunks, embeds and persists one document. The
// document row is written before its chunks so a failed chunk write is rolled
// back by deleting the row.
func (s *Service) Ingest(
	ctx context.Context,
	projectID, fileName, contentType string,
	content []byte,
	opts IngestOptions,
) (*domainrag.Document, error) {
	projectID, fileName, err := validateUpload(projectID, fileName, content)
	if err != nil {
		return nil, err
	}
	settings, err := s.resolvedSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	opts = applyIngestSettings(settings, opts)
	text, err := s.extractor.Extract(ctx, projectID, fileName, content)
	if err != nil {
		return nil, err
	}
	passages := domainrag.ChunkText(text, opts.ChunkSize, opts.ChunkOverlap)
	if len(passages) == 0 {
		return nil, &domain.InvalidRequestError{Param: "file", Reason: "document has no readable text"}
	}
	vectors, model, err := s.embedTexts(ctx, projectID, passages, opts.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	doc := newDocument(s, projectID, fileName, contentType, len(content), len(passages), model)
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	if err := s.persist(ctx, doc, buildChunks(s, doc, passages, vectors)); err != nil {
		return nil, err
	}
	return doc, nil
}

func validateUpload(projectID, fileName string, content []byte) (string, string, error) {
	projectID = trimProject(projectID)
	fileName = path.Base(strings.TrimSpace(fileName))
	if projectID == "" {
		return "", "", &domain.ValidationError{Fields: []string{"project_id"}}
	}
	if fileName == "" || fileName == "." || fileName == "/" {
		return "", "", &domain.ValidationError{Fields: []string{"file_name"}}
	}
	if len(content) == 0 {
		return "", "", &domain.InvalidRequestError{Param: "file", Reason: "file is empty"}
	}
	return projectID, fileName, nil
}

func newDocument(s *Service, projectID, fileName, contentType string, size, chunks int, model string) *domainrag.Document {
	return &domainrag.Document{
		ID:             s.ids(),
		ProjectID:      projectID,
		FileName:       fileName,
		ContentType:    strings.TrimSpace(contentType),
		SizeBytes:      size,
		ChunkCount:     chunks,
		EmbeddingModel: model,
		CreatedAt:      s.now().UTC(),
	}
}

func buildChunks(s *Service, doc *domainrag.Document, passages []string, vectors [][]float32) []domainrag.Chunk {
	chunks := make([]domainrag.Chunk, len(passages))
	for i, text := range passages {
		chunks[i] = domainrag.Chunk{
			ID:             doc.ID + ":" + strconv.Itoa(i),
			DocumentID:     doc.ID,
			ProjectID:      doc.ProjectID,
			FileName:       doc.FileName,
			Ordinal:        i,
			Text:           text,
			EmbeddingModel: doc.EmbeddingModel,
			Vector:         vectors[i],
			CreatedAt:      doc.CreatedAt,
		}
	}
	return chunks
}

func (s *Service) persist(ctx context.Context, doc *domainrag.Document, chunks []domainrag.Chunk) error {
	if err := s.docs.Create(ctx, doc); err != nil {
		return err
	}
	if err := s.chunks.ReplaceForDocument(ctx, doc.ProjectID, doc.ID, chunks); err != nil {
		_ = s.docs.Delete(ctx, doc.ID)
		return err
	}
	return nil
}

// Documents lists the indexed documents of a project, newest first.
func (s *Service) Documents(ctx context.Context, projectID string) ([]domainrag.Document, error) {
	projectID = trimProject(projectID)
	if projectID == "" {
		return nil, &domain.ValidationError{Fields: []string{"project_id"}}
	}
	return s.docs.ListByProject(ctx, projectID)
}

// DeleteDocument removes a document and every passage derived from it.
func (s *Service) DeleteDocument(ctx context.Context, projectID, documentID string) error {
	projectID = trimProject(projectID)
	documentID = strings.TrimSpace(documentID)
	doc, err := s.docs.Get(ctx, documentID)
	if err != nil {
		return err
	}
	if doc.ProjectID != projectID {
		return domainrag.ErrDocumentNotFound
	}
	if err := s.chunks.DeleteByDocument(ctx, projectID, documentID); err != nil {
		return err
	}
	return s.docs.Delete(ctx, documentID)
}
