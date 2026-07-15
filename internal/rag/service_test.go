package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeEmbedder struct {
	calls [][]string
	err   error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls = append(f.calls, append([]string(nil), texts...))
	if f.err != nil {
		return nil, f.err
	}
	vectors := make([][]float32, len(texts))
	for index := range texts {
		vectors[index] = []float32{float32(len([]rune(texts[index]))), float32(index + 1), 1}
	}
	return vectors, nil
}

type fakeVectorStore struct {
	vectorSize int
	points     []VectorPoint
	query      []float32
	limit      int
	results    []SearchResult
	err        error
}

func (f *fakeVectorStore) Upsert(_ context.Context, vectorSize int, points []VectorPoint) error {
	f.vectorSize = vectorSize
	f.points = points
	return f.err
}

func (f *fakeVectorStore) Query(_ context.Context, vector []float32, limit int) ([]SearchResult, error) {
	f.query = vector
	f.limit = limit
	return f.results, f.err
}

func TestServiceImportChunksEmbedsAndStoresDocument(t *testing.T) {
	embedder := &fakeEmbedder{}
	store := &fakeVectorStore{}
	service := NewService(embedder, store)
	content := []byte(strings.Repeat("知", 801))

	result, err := service.Import(context.Background(), " policy.MD ", content)
	if err != nil {
		t.Fatalf("Import() returned error: %v", err)
	}
	if result.Source != "policy.MD" || result.ChunkCount != 3 || len(result.DocumentID) != 64 {
		t.Fatalf("Import() result = %#v", result)
	}
	if len(embedder.calls) != 1 || len(embedder.calls[0]) != 3 {
		t.Fatalf("embed calls = %#v", embedder.calls)
	}
	if store.vectorSize != 3 || len(store.points) != 3 || store.points[1].ChunkIndex != 1 {
		t.Fatalf("stored points = %#v size=%d", store.points, store.vectorSize)
	}
	second, err := service.Import(context.Background(), "policy.MD", content)
	if err != nil || second.DocumentID != result.DocumentID || store.points[0].ID == "" {
		t.Fatalf("deterministic retry failed: first=%#v second=%#v err=%v", result, second, err)
	}
}

func TestServiceImportBatchesEmbeddings(t *testing.T) {
	embedder := &fakeEmbedder{}
	service := NewService(embedder, &fakeVectorStore{})
	content := []byte(strings.Repeat("a", DefaultChunkSize+(DefaultChunkSize-DefaultChunkOverlap)*embeddingBatchSize))

	result, err := service.Import(context.Background(), "large.txt", content)
	if err != nil {
		t.Fatalf("Import() returned error: %v", err)
	}
	if result.ChunkCount != embeddingBatchSize+1 || len(embedder.calls) != 2 {
		t.Fatalf("chunks=%d embed calls=%d", result.ChunkCount, len(embedder.calls))
	}
}

func TestServiceImportRejectsInvalidDocumentsBeforeDependencies(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  []byte
	}{
		{name: "extension", filename: "file.pdf", content: []byte("text")},
		{name: "empty", filename: "file.txt"},
		{name: "invalid UTF-8", filename: "file.md", content: []byte{0xff}},
		{name: "too large", filename: "file.txt", content: make([]byte, MaxDocumentBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			embedder := &fakeEmbedder{}
			store := &fakeVectorStore{}
			_, err := NewService(embedder, store).Import(context.Background(), test.filename, test.content)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Import() error = %v, want ErrInvalidInput", err)
			}
			if len(embedder.calls) != 0 || len(store.points) != 0 {
				t.Fatal("invalid document reached dependencies")
			}
		})
	}
}

func TestServiceMapsEmbeddingAndStoreErrors(t *testing.T) {
	embeddingErr := errors.New("model unavailable")
	_, err := NewService(&fakeEmbedder{err: embeddingErr}, &fakeVectorStore{}).Import(context.Background(), "file.txt", []byte("text"))
	if !errors.Is(err, ErrEmbedding) || !errors.Is(err, embeddingErr) {
		t.Fatalf("embedding error = %v", err)
	}
	storeErr := errors.New("Qdrant unavailable")
	_, err = NewService(&fakeEmbedder{}, &fakeVectorStore{err: storeErr}).Import(context.Background(), "file.txt", []byte("text"))
	if !errors.Is(err, ErrVectorStore) || !errors.Is(err, storeErr) {
		t.Fatalf("store error = %v", err)
	}
}

func TestServiceSearchUsesDefaultAndValidatedTopK(t *testing.T) {
	store := &fakeVectorStore{results: []SearchResult{{Source: "policy.md", Text: "refund"}}}
	service := NewService(&fakeEmbedder{}, store)
	results, err := service.Search(context.Background(), " refund ", 0)
	if err != nil {
		t.Fatalf("Search() returned error: %v", err)
	}
	if len(results) != 1 || store.limit != DefaultTopK || len(store.query) != 3 {
		t.Fatalf("results=%#v limit=%d query=%v", results, store.limit, store.query)
	}
	for _, input := range []struct {
		query string
		topK  int
	}{{query: " "}, {query: "q", topK: MaxTopK + 1}} {
		if _, err := service.Search(context.Background(), input.query, input.topK); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Search(%q, %d) error = %v", input.query, input.topK, err)
		}
	}
}
