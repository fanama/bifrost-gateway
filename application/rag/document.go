package rag

import (
	"context"
	"sort"
	"strings"

	domainrag "bridge-gateway/domain/rag"
)

// DocumentChunks returns one document and its stored passages in reading
// order. The document must belong to the caller's project, exactly like
// deletion, so a document id alone cannot read another tenant's passages.
func (s *Service) DocumentChunks(ctx context.Context, projectID, documentID string) (*domainrag.Document, []domainrag.Chunk, error) {
	projectID = trimProject(projectID)
	documentID = strings.TrimSpace(documentID)
	doc, err := s.docs.Get(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if doc.ProjectID != projectID {
		return nil, nil, domainrag.ErrDocumentNotFound
	}
	chunks, err := s.chunks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	own := make([]domainrag.Chunk, 0, doc.ChunkCount)
	for _, chunk := range chunks {
		if chunk.DocumentID == documentID {
			own = append(own, chunk)
		}
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].Ordinal < own[j].Ordinal })
	return doc, own, nil
}
