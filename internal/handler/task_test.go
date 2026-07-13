package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/service"
)

type fakeTaskCreator struct {
	input service.CreateTaskInput
	task  *domain.Task
	err   error
	calls int
}

func (f *fakeTaskCreator) Create(_ context.Context, input service.CreateTaskInput) (*domain.Task, error) {
	f.calls++
	f.input = input
	return f.task, f.err
}

func TestTaskHandlerCreateReturns201(t *testing.T) {
	creator := &fakeTaskCreator{task: &domain.Task{ID: 1, Name: "report", Status: domain.StatusPending}}
	response := performCreateRequest(t, creator, `{
		"name":"report",
		"steps":[{"name":"wait","action_type":"sleep","action_payload":{"duration_ms":100}}]
	}`)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if creator.calls != 1 {
		t.Fatalf("service calls = %d, want 1", creator.calls)
	}
	if creator.input.Name != "report" || len(creator.input.Steps) != 1 {
		t.Fatalf("unexpected service input: %#v", creator.input)
	}
}

func TestTaskHandlerCreateRejectsMalformedJSONBeforeService(t *testing.T) {
	creator := &fakeTaskCreator{}
	response := performCreateRequest(t, creator, `{"name":`)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if creator.calls != 0 {
		t.Fatalf("service calls = %d, want 0", creator.calls)
	}
}

func TestTaskHandlerCreateMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{name: "invalid input", err: service.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantError: "invalid input"},
		{name: "database failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantError: "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeTaskCreator{err: tt.err}
			response := performCreateRequest(t, creator, `{"name":"task","steps":[]}`)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error"] != tt.wantError {
				t.Fatalf("error = %q, want %q", body["error"], tt.wantError)
			}
		})
	}
}

func performCreateRequest(t *testing.T, creator TaskCreator, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/tasks", NewTaskHandler(creator).Create)

	request := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
