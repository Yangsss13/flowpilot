package handler

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/rag"
)

type fakeKnowledgeApplication struct {
	filename string
	content  []byte
	query    string
	topK     int
	imported rag.ImportResult
	results  []rag.SearchResult
	err      error
	calls    int
}

func (f *fakeKnowledgeApplication) Import(_ context.Context, filename string, content []byte) (rag.ImportResult, error) {
	f.calls++
	f.filename = filename
	f.content = content
	return f.imported, f.err
}

func (f *fakeKnowledgeApplication) Search(_ context.Context, query string, topK int) ([]rag.SearchResult, error) {
	f.calls++
	f.query = query
	f.topK = topK
	return f.results, f.err
}

func TestKnowledgeHandlerImportsDocument(t *testing.T) {
	application := &fakeKnowledgeApplication{imported: rag.ImportResult{DocumentID: "doc", Source: "policy.md", ChunkCount: 2}}
	response := performKnowledgeImport(t, application, "policy.md", []byte("refund policy"))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", response.Code, response.Body.String())
	}
	if application.calls != 1 || application.filename != "policy.md" || string(application.content) != "refund policy" {
		t.Fatalf("application call: calls=%d filename=%q content=%q", application.calls, application.filename, application.content)
	}
}

func TestKnowledgeHandlerRejectsMissingFile(t *testing.T) {
	application := &fakeKnowledgeApplication{}
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/documents", nil)
	response := httptest.NewRecorder()
	newKnowledgeTestRouter(application).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, application.calls)
	}
}

func TestKnowledgeHandlerSearches(t *testing.T) {
	application := &fakeKnowledgeApplication{results: []rag.SearchResult{{Source: "policy.md", Text: "refund"}}}
	response := performKnowledgeSearch(application, `{"query":"refund","top_k":3}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if application.calls != 1 || application.query != "refund" || application.topK != 3 {
		t.Fatalf("application call: calls=%d query=%q topK=%d", application.calls, application.query, application.topK)
	}
}

func TestKnowledgeHandlerMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid", err: rag.ErrInvalidInput, wantStatus: http.StatusBadRequest},
		{name: "embedding", err: rag.ErrEmbedding, wantStatus: http.StatusBadGateway},
		{name: "Qdrant", err: rag.ErrVectorStore, wantStatus: http.StatusServiceUnavailable},
		{name: "internal", err: errors.New("unexpected"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performKnowledgeSearch(&fakeKnowledgeApplication{err: test.err}, `{"query":"q"}`)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestKnowledgeHandlerRejectsMalformedSearchJSON(t *testing.T) {
	application := &fakeKnowledgeApplication{}
	response := performKnowledgeSearch(application, `{"query":`)
	if response.Code != http.StatusBadRequest || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, application.calls)
	}
}

func performKnowledgeImport(t *testing.T, application KnowledgeApplication, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/documents", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	newKnowledgeTestRouter(application).ServeHTTP(response, request)
	return response
}

func performKnowledgeSearch(application KnowledgeApplication, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/search", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newKnowledgeTestRouter(application).ServeHTTP(response, request)
	return response
}

func newKnowledgeTestRouter(application KnowledgeApplication) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewKnowledgeHandler(application)
	router.POST("/api/knowledge/documents", handler.Import)
	router.POST("/api/knowledge/search", handler.Search)
	return router
}
