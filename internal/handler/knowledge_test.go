package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/knowledge"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type fakeKnowledgeApplication struct {
	filename string
	content  []byte
	search   knowledge.SearchRequest
	uploaded knowledge.UploadResult
	results  []rag.SearchResult
	err      error
	calls    int
	cancelID uint64
}

func (f *fakeKnowledgeApplication) Upload(_ context.Context, input knowledge.UploadInput) (knowledge.UploadResult, error) {
	f.calls++
	f.filename = input.Filename
	f.content, _ = io.ReadAll(input.Source)
	return f.uploaded, f.err
}

func (f *fakeKnowledgeApplication) UploadVersion(ctx context.Context, _ uint64, input knowledge.UploadInput) (knowledge.UploadResult, error) {
	return f.Upload(ctx, input)
}

func (f *fakeKnowledgeApplication) List(context.Context, knowledge.DocumentFilter) (knowledge.DocumentList, error) {
	return knowledge.DocumentList{}, f.err
}

func (f *fakeKnowledgeApplication) Get(context.Context, uint64) (knowledge.DocumentDetail, error) {
	return knowledge.DocumentDetail{}, f.err
}

func (f *fakeKnowledgeApplication) Delete(context.Context, uint64) error { return f.err }

func (f *fakeKnowledgeApplication) GetJob(context.Context, uint64) (domain.IngestionJob, error) {
	return domain.IngestionJob{}, f.err
}

func (f *fakeKnowledgeApplication) Retry(context.Context, uint64) (domain.IngestionJob, error) {
	return domain.IngestionJob{}, f.err
}

func (f *fakeKnowledgeApplication) Reindex(_ context.Context, id uint64) (domain.IngestionJob, error) {
	return domain.IngestionJob{ID: 9, DocumentID: id, Status: domain.IngestionJobQueued}, f.err
}

func (f *fakeKnowledgeApplication) Cancel(_ context.Context, id uint64) (domain.IngestionJob, error) {
	f.cancelID = id
	return domain.IngestionJob{ID: id, Status: domain.IngestionJobCanceled}, f.err
}

func (f *fakeKnowledgeApplication) Search(_ context.Context, request knowledge.SearchRequest) ([]rag.SearchResult, error) {
	f.calls++
	f.search = request
	return f.results, f.err
}

func TestKnowledgeHandlerAcceptsDocumentAsynchronously(t *testing.T) {
	application := &fakeKnowledgeApplication{uploaded: knowledge.UploadResult{DocumentID: 1, VersionID: 2, JobID: 3, Status: domain.IngestionJobQueued}}
	response := performKnowledgeImport(t, application, "policy.md", []byte("refund policy"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", response.Code, response.Body.String())
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

func TestKnowledgeHandlerSearchesWithThreshold(t *testing.T) {
	application := &fakeKnowledgeApplication{results: []rag.SearchResult{{Source: "policy.md", Text: "refund"}}}
	response := performKnowledgeSearch(application, `{"query":"refund","top_k":3,"min_score":0.6}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if application.calls != 1 || application.search.Query != "refund" || application.search.TopK != 3 || application.search.MinScore != 0.6 {
		t.Fatalf("application call: calls=%d search=%#v", application.calls, application.search)
	}
}

func TestKnowledgeHandlerRejectsUnknownSearchField(t *testing.T) {
	application := &fakeKnowledgeApplication{}
	response := performKnowledgeSearch(application, `{"query":"q","unknown":true}`)
	if response.Code != http.StatusBadRequest || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, application.calls)
	}
}

func TestKnowledgeHandlerCancelsIngestionJob(t *testing.T) {
	application := &fakeKnowledgeApplication{}
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/jobs/7/cancel", nil)
	response := httptest.NewRecorder()
	newKnowledgeTestRouter(application).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || application.cancelID != 7 || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"Canceled"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestKnowledgeHandlerRejectsInvalidCancelJobID(t *testing.T) {
	application := &fakeKnowledgeApplication{}
	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/jobs/not-a-number/cancel", nil)
	response := httptest.NewRecorder()
	newKnowledgeTestRouter(application).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || application.cancelID != 0 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestKnowledgeHandlerMapsSafeErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid", err: knowledge.ErrInvalidInput, wantStatus: http.StatusBadRequest},
		{name: "missing", err: knowledge.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "conflict", err: knowledge.ErrConflict, wantStatus: http.StatusConflict},
		{name: "embedding", err: rag.ErrEmbedding, wantStatus: http.StatusBadGateway},
		{name: "Qdrant", err: rag.ErrVectorStore, wantStatus: http.StatusServiceUnavailable},
		{name: "internal", err: errors.New("C:\\secret\\object sk-private"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performKnowledgeSearch(&fakeKnowledgeApplication{err: test.err}, `{"query":"q"}`)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if bytes.Contains(response.Body.Bytes(), []byte("secret")) || bytes.Contains(response.Body.Bytes(), []byte("sk-private")) {
				t.Fatalf("response leaked internal error: %s", response.Body.String())
			}
		})
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
	_, _ = part.Write(content)
	_ = writer.Close()
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
	handler := NewKnowledgeHandler(application, 50<<20)
	router.POST("/api/knowledge/documents", handler.Import)
	router.POST("/api/knowledge/jobs/:id/cancel", handler.Cancel)
	router.POST("/api/knowledge/search", handler.Search)
	return router
}
