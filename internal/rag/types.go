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
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type VectorPoint struct {
	ID         string
	DocumentID string
	Source     string
	ChunkIndex int
	Text       string
	Vector     []float32
}

type SearchResult struct {
	DocumentID string  `json:"document_id"`
	Source     string  `json:"source"`
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
