package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleEmbedderReturnsVectorsInInputOrder(t *testing.T) {
	var received embeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/embeddings" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": []any{
			map[string]any{"index": 1, "embedding": []float32{3, 4}},
			map[string]any{"index": 0, "embedding": []float32{1, 2}},
		}})
	}))
	defer server.Close()
	embedder := newTestEmbedder(t, server.URL+"/v1", server.Client())

	vectors, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors = %v", vectors)
	}
	if received.Model != "test-model" || received.EncodingFormat != "float" || len(received.Input) != 2 {
		t.Fatalf("request = %#v", received)
	}
}

func TestOpenAICompatibleEmbedderRejectsInvalidResponsesWithoutLeakingKey(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "HTTP error", status: http.StatusUnauthorized, body: `{"error":"test-key"}`, want: "HTTP status 401"},
		{name: "malformed", status: http.StatusOK, body: `{`, want: "decode embedding response"},
		{name: "wrong count", status: http.StatusOK, body: `{"data":[]}`, want: "0 vectors for 1 inputs"},
		{name: "invalid index", status: http.StatusOK, body: `{"data":[{"index":2,"embedding":[1]}]}`, want: "invalid index"},
		{name: "empty vector", status: http.StatusOK, body: `{"data":[{"index":0,"embedding":[]}]}`, want: "empty vector"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			embedder := newTestEmbedder(t, server.URL, server.Client())
			_, err := embedder.Embed(context.Background(), []string{"text"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Embed() error = %v, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Fatalf("Embed() leaked API key: %v", err)
			}
		})
	}
}

func TestOpenAICompatibleEmbedderValidatesInputAndConfiguration(t *testing.T) {
	if _, err := NewOpenAICompatibleEmbedder("localhost", "key", "model", nil); err == nil {
		t.Fatal("constructor accepted invalid URL")
	}
	if _, err := NewOpenAICompatibleEmbedder("https://example.com/v1", "", "model", nil); err == nil {
		t.Fatal("constructor accepted empty key")
	}
	if _, err := NewOpenAICompatibleEmbedder("https://example.com/v1", "key", "", nil); err == nil {
		t.Fatal("constructor accepted empty model")
	}
	embedder, err := NewOpenAICompatibleEmbedder("https://example.com/v1", "key", "model", nil)
	if err != nil {
		t.Fatalf("constructor returned error: %v", err)
	}
	if _, err := embedder.Embed(context.Background(), nil); err == nil {
		t.Fatal("Embed() accepted empty batch")
	}
	if _, err := embedder.Embed(context.Background(), []string{" "}); err == nil {
		t.Fatal("Embed() accepted empty text")
	}
}

func newTestEmbedder(t *testing.T, baseURL string, client *http.Client) *OpenAICompatibleEmbedder {
	t.Helper()
	embedder, err := NewOpenAICompatibleEmbedder(baseURL, "test-key", "test-model", client)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleEmbedder() returned error: %v", err)
	}
	return embedder
}
