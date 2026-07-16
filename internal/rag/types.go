package rag

import (
	"context"
	"errors"
)

const DefaultChunkSize = 400
const DefaultChunkOverlap = 50
const DefaultTopK = 5
const MaxTopK = 10
const MaxDocumentBytes = 1 << 20

var ErrInvalidInput = errors.New("invalid RAG input")
var ErrEmbedding = errors.New("embedding failed")
var ErrVectorStore = errors.New("vector store failed")

type Chunk struct {
	Index   int    `json:"index"`
	Text    string `json:"text"`
	Section string `json:"section,omitempty"`
	Page    int    `json:"page,omitempty"`
	Slide   int    `json:"slide,omitempty"`
	StartMS int64  `json:"start_ms,omitempty"`
	EndMS   int64  `json:"end_ms,omitempty"`
}

type VectorPoint struct {
	ID         string
	DocumentID string
	VersionID  uint64
	Source     string
	Section    string
	Page       int
	Slide      int
	StartMS    int64
	EndMS      int64
	ChunkIndex int
	Text       string
	Vector     []float32
}

type SearchResult struct {
	DocumentID string  `json:"document_id"`
	VersionID  uint64  `json:"version_id,omitempty"`
	Source     string  `json:"source"`
	Section    string  `json:"section,omitempty"`
	Page       int     `json:"page,omitempty"`
	Slide      int     `json:"slide,omitempty"`
	StartMS    int64   `json:"start_ms,omitempty"`
	EndMS      int64   `json:"end_ms,omitempty"`
	StartTime  string  `json:"start_time,omitempty"`
	EndTime    string  `json:"end_time,omitempty"`
	ChunkIndex int     `json:"chunk_index"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

type ImportResult struct {
	DocumentID string `json:"document_id"`
	Source     string `json:"source"`
	ChunkCount int    `json:"chunk_count"`
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type VectorStore interface {
	Upsert(ctx context.Context, vectorSize int, points []VectorPoint) error
	Query(ctx context.Context, vector []float32, limit int) ([]SearchResult, error)
}

type ThresholdVectorStore interface {
	QueryWithThreshold(ctx context.Context, vector []float32, limit int, minScore float64) ([]SearchResult, error)
}

type VectorCleaner interface {
	DeleteDocument(ctx context.Context, documentID string) error
	DeleteVersion(ctx context.Context, documentID string, versionID uint64) error
}
