package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const embeddingBatchSize = 32

type Service struct {
	embedder Embedder
	store    VectorStore
}

func NewService(embedder Embedder, store VectorStore) *Service {
	return &Service{embedder: embedder, store: store}
}

func (s *Service) Import(ctx context.Context, filename string, content []byte) (ImportResult, error) {
	filename = strings.TrimSpace(path.Base(strings.ReplaceAll(filename, "\\", "/")))
	extension := strings.ToLower(path.Ext(filename))
	if filename == "" || filename == "." || (extension != ".txt" && extension != ".md") {
		return ImportResult{}, fmt.Errorf("%w: only .txt and .md files are supported", ErrInvalidInput)
	}
	if len(content) == 0 {
		return ImportResult{}, fmt.Errorf("%w: document is empty", ErrInvalidInput)
	}
	if len(content) > MaxDocumentBytes {
		return ImportResult{}, fmt.Errorf("%w: document exceeds %d bytes", ErrInvalidInput, MaxDocumentBytes)
	}
	if !utf8.Valid(content) {
		return ImportResult{}, fmt.Errorf("%w: document must be UTF-8 text", ErrInvalidInput)
	}
	chunks, err := ChunkText(string(content), DefaultChunkSize, DefaultChunkOverlap)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	texts := make([]string, len(chunks))
	for index := range chunks {
		texts[index] = chunks[index].Text
	}
	vectors, err := s.embedAll(ctx, texts)
	if err != nil {
		return ImportResult{}, err
	}
	documentID := documentDigest(filename, content)
	points := make([]VectorPoint, len(chunks))
	for index := range chunks {
		points[index] = VectorPoint{
			ID:         deterministicPointID(documentID, chunks[index].Index),
			DocumentID: documentID,
			Source:     filename,
			ChunkIndex: chunks[index].Index,
			Text:       chunks[index].Text,
			Vector:     vectors[index],
		}
	}
	if err := s.store.Upsert(ctx, len(vectors[0]), points); err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", ErrVectorStore, err)
	}
	return ImportResult{DocumentID: documentID, Source: filename, ChunkCount: len(chunks)}, nil
}

func (s *Service) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	return s.SearchAdvanced(ctx, query, topK, 0)
}

func (s *Service) SearchAdvanced(ctx context.Context, query string, topK int, minScore float64) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidInput)
	}
	if topK == 0 {
		topK = DefaultTopK
	}
	if topK < 1 || topK > MaxTopK {
		return nil, fmt.Errorf("%w: top_k must be between 1 and %d", ErrInvalidInput, MaxTopK)
	}
	if minScore < 0 || minScore > 1 {
		return nil, fmt.Errorf("%w: min_score must be between 0 and 1", ErrInvalidInput)
	}
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmbedding, err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("%w: provider returned an invalid query vector", ErrEmbedding)
	}
	var results []SearchResult
	if thresholdStore, ok := s.store.(ThresholdVectorStore); ok {
		results, err = thresholdStore.QueryWithThreshold(ctx, vectors[0], topK, minScore)
	} else {
		results, err = s.store.Query(ctx, vectors[0], topK)
		if err == nil && minScore > 0 {
			filtered := results[:0]
			for _, result := range results {
				if result.Score >= minScore {
					filtered = append(filtered, result)
				}
			}
			results = filtered
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVectorStore, err)
	}
	return results, nil
}

func (s *Service) IndexDocument(ctx context.Context, documentID string, versionID uint64, source string, chunks []Chunk) error {
	if strings.TrimSpace(documentID) == "" || versionID == 0 || strings.TrimSpace(source) == "" || len(chunks) == 0 {
		return fmt.Errorf("%w: document metadata and chunks are required", ErrInvalidInput)
	}
	texts := make([]string, len(chunks))
	for index := range chunks {
		texts[index] = chunks[index].Text
	}
	vectors, err := s.embedAll(ctx, texts)
	if err != nil {
		return err
	}
	points := make([]VectorPoint, len(chunks))
	for index := range chunks {
		points[index] = VectorPoint{
			ID: deterministicVersionPointID(documentID, versionID, chunks[index].Index), DocumentID: documentID,
			VersionID: versionID, Source: source, ChunkIndex: chunks[index].Index, Text: chunks[index].Text,
			Section: chunks[index].Section, Page: chunks[index].Page, Slide: chunks[index].Slide,
			StartMS: chunks[index].StartMS, EndMS: chunks[index].EndMS,
			Vector: vectors[index],
		}
	}
	if err := s.store.Upsert(ctx, len(vectors[0]), points); err != nil {
		return fmt.Errorf("%w: %w", ErrVectorStore, err)
	}
	return nil
}

func (s *Service) DeleteDocument(ctx context.Context, documentID string) error {
	cleaner, ok := s.store.(VectorCleaner)
	if !ok {
		return fmt.Errorf("%w: vector store does not support deletion", ErrVectorStore)
	}
	if err := cleaner.DeleteDocument(ctx, documentID); err != nil {
		return fmt.Errorf("%w: %w", ErrVectorStore, err)
	}
	return nil
}

func (s *Service) DeleteVersion(ctx context.Context, documentID string, versionID uint64) error {
	cleaner, ok := s.store.(VectorCleaner)
	if !ok {
		return fmt.Errorf("%w: vector store does not support deletion", ErrVectorStore)
	}
	if err := cleaner.DeleteVersion(ctx, documentID, versionID); err != nil {
		return fmt.Errorf("%w: %w", ErrVectorStore, err)
	}
	return nil
}

func (s *Service) embedAll(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	dimension := 0
	for start := 0; start < len(texts); start += embeddingBatchSize {
		end := min(start+embeddingBatchSize, len(texts))
		batch, err := s.embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEmbedding, err)
		}
		if len(batch) != end-start {
			return nil, fmt.Errorf("%w: provider returned %d vectors for %d chunks", ErrEmbedding, len(batch), end-start)
		}
		for _, vector := range batch {
			if len(vector) == 0 {
				return nil, fmt.Errorf("%w: provider returned an empty vector", ErrEmbedding)
			}
			if dimension == 0 {
				dimension = len(vector)
			} else if len(vector) != dimension {
				return nil, fmt.Errorf("%w: provider returned inconsistent vector dimensions", ErrEmbedding)
			}
			vectors = append(vectors, vector)
		}
	}
	return vectors, nil
}

func documentDigest(filename string, content []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(filename))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(content)
	return hex.EncodeToString(hash.Sum(nil))
}

func deterministicPointID(documentID string, chunkIndex int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", documentID, chunkIndex)))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(value)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[0:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:32])
}

func deterministicVersionPointID(documentID string, versionID uint64, chunkIndex int) string {
	return deterministicPointID(fmt.Sprintf("%s:%d", documentID, versionID), chunkIndex)
}
