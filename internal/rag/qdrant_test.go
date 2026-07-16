package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantStoreCreatesCollectionUpsertsAndQueries(t *testing.T) {
	collectionExists := false
	upserted := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("api-key") != "qdrant-key" {
			t.Errorf("api-key = %q", request.Header.Get("api-key"))
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/collections/knowledge":
			if !collectionExists {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = writer.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":3}}}}}`))
		case request.Method == http.MethodPut && request.URL.Path == "/collections/knowledge":
			var body struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body.Vectors.Size != 3 || body.Vectors.Distance != "Cosine" {
				t.Errorf("create body = %#v", body)
			}
			collectionExists = true
			_, _ = writer.Write([]byte(`{"result":true}`))
		case request.Method == http.MethodPut && request.URL.Path == "/collections/knowledge/points":
			if request.URL.Query().Get("wait") != "true" {
				t.Error("upsert did not request wait=true")
			}
			var body struct {
				Points []json.RawMessage `json:"points"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			upserted = len(body.Points)
			_, _ = writer.Write([]byte(`{"result":{"status":"completed"}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/collections/knowledge/points/query":
			_, _ = writer.Write([]byte(`{"result":{"points":[{"score":0.91,"payload":{"document_id":"doc","source":"policy.md","chunk_index":0,"text":"refund policy"}}]}}`))
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	store, err := NewQdrantStore(server.URL, "knowledge", "qdrant-key", server.Client())
	if err != nil {
		t.Fatalf("NewQdrantStore() returned error: %v", err)
	}

	err = store.Upsert(context.Background(), 3, []VectorPoint{{
		ID: "11111111-1111-5111-8111-111111111111", DocumentID: "doc", Source: "policy.md", Text: "refund policy", Vector: []float32{1, 0, 0},
	}})
	if err != nil || upserted != 1 {
		t.Fatalf("Upsert() error=%v upserted=%d", err, upserted)
	}
	results, err := store.Query(context.Background(), []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("Query() returned error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "policy.md" || results[0].Score != 0.91 {
		t.Fatalf("Query() results = %#v", results)
	}
}

func TestQdrantStoreSendsThresholdMetadataAndDeleteFilters(t *testing.T) {
	var sawThreshold, sawVersionDelete, sawDocumentDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		switch {
		case strings.HasSuffix(request.URL.Path, "/points/query"):
			sawThreshold = body["score_threshold"] == 0.65
			_, _ = writer.Write([]byte(`{"result":{"points":[{"score":0.9,"payload":{"document_id":"7","version_id":2,"source":"demo.mp4","section":"Revenue","slide":3,"start_ms":192000,"end_ms":220000,"chunk_index":1,"text":"growth"}}]}}`))
		case strings.HasSuffix(request.URL.Path, "/points/delete"):
			encoded, _ := json.Marshal(body)
			value := string(encoded)
			if strings.Contains(value, `"version_id"`) {
				sawVersionDelete = strings.Contains(value, `"document_id"`)
			} else {
				sawDocumentDelete = strings.Contains(value, `"document_id"`)
			}
			_, _ = writer.Write([]byte(`{"result":true}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	store, err := NewQdrantStore(server.URL, "knowledge", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	results, err := store.QueryWithThreshold(context.Background(), []float32{1, 0}, 5, 0.65)
	if err != nil || len(results) != 1 || results[0].VersionID != 2 || results[0].Slide != 3 || results[0].Section != "Revenue" ||
		results[0].StartTime != "00:03:12" || results[0].EndTime != "00:03:40" {
		t.Fatalf("results=%#v error=%v", results, err)
	}
	if err := store.DeleteVersion(context.Background(), "7", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDocument(context.Background(), "7"); err != nil {
		t.Fatal(err)
	}
	if !sawThreshold || !sawVersionDelete || !sawDocumentDelete {
		t.Fatalf("threshold=%v versionDelete=%v documentDelete=%v", sawThreshold, sawVersionDelete, sawDocumentDelete)
	}
}

func TestQdrantStoreRejectsCollectionDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":2}}}}}`))
	}))
	defer server.Close()
	store, err := NewQdrantStore(server.URL, "knowledge", "", server.Client())
	if err != nil {
		t.Fatalf("NewQdrantStore() returned error: %v", err)
	}
	err = store.Upsert(context.Background(), 3, []VectorPoint{{ID: "id", Vector: []float32{1, 2, 3}}})
	if err == nil || !strings.Contains(err.Error(), "vector size is 2") {
		t.Fatalf("Upsert() error = %v", err)
	}
}

func TestNewQdrantStoreValidatesConfiguration(t *testing.T) {
	if _, err := NewQdrantStore("localhost:6333", "knowledge", "", nil); err == nil {
		t.Fatal("constructor accepted invalid URL")
	}
	if _, err := NewQdrantStore("http://localhost:6333", "bad/name", "", nil); err == nil {
		t.Fatal("constructor accepted invalid collection")
	}
}

func TestQdrantStoreHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store, err := NewQdrantStore(server.URL, "knowledge", "", server.Client())
	if err != nil {
		t.Fatalf("NewQdrantStore() error = %v", err)
	}
	if err := store.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}
