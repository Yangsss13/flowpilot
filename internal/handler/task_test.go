package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type fakeTaskCreator struct {
	input     service.CreateTaskInput
	task      *domain.Task
	page      service.TaskListResult
	stats     service.TaskStatsResult
	err       error
	calls     int
	deletedID uint64
}

func (f *fakeTaskCreator) Create(_ context.Context, input service.CreateTaskInput) (*domain.Task, error) {
	f.calls++
	f.input = input
	return f.task, f.err
}

func (f *fakeTaskCreator) List(_ context.Context, _ service.ListTasksInput) (service.TaskListResult, error) {
	f.calls++
	return f.page, f.err
}

func (f *fakeTaskCreator) Stats(_ context.Context) (service.TaskStatsResult, error) {
	f.calls++
	return f.stats, f.err
}

func (f *fakeTaskCreator) GetByID(_ context.Context, _ uint64) (*domain.Task, error) {
	f.calls++
	return f.task, f.err
}

func (f *fakeTaskCreator) Delete(_ context.Context, id uint64) error {
	f.calls++
	f.deletedID = id
	return f.err
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

func TestTaskHandlerCreateRejectsUnknownFieldsAndOversizedBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"task","unknown":true,"steps":[]}`},
		{name: "oversized body", body: `{"name":"task","description":"` + strings.Repeat("x", service.MaxTaskRequestBytes) + `","steps":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeTaskCreator{}
			response := performCreateRequest(t, creator, tt.body)
			if response.Code != http.StatusBadRequest || creator.calls != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, creator.calls)
			}
		})
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

func TestTaskHandlerListReturnsTasks(t *testing.T) {
	creator := &fakeTaskCreator{page: service.TaskListResult{Items: []service.TaskListItem{{ID: 2, Name: "newest", Status: domain.StatusPending}}, Total: 1, Page: 1, PageSize: 20}}
	response := performRequest(t, creator, http.MethodGet, "/api/tasks", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if creator.calls != 1 {
		t.Fatalf("service calls = %d, want 1", creator.calls)
	}
}

func TestTaskHandlerListRejectsInvalidPagination(t *testing.T) {
	creator := &fakeTaskCreator{}
	response := performRequest(t, creator, http.MethodGet, "/api/tasks?page=0", "")
	if response.Code != http.StatusBadRequest || creator.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, creator.calls)
	}
}

func TestTaskHandlerGetByID(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		task       *domain.Task
		err        error
		wantStatus int
		wantCalls  int
	}{
		{name: "success", path: "/api/tasks/1", task: &domain.Task{ID: 1, Name: "task"}, wantStatus: http.StatusOK, wantCalls: 1},
		{name: "invalid id", path: "/api/tasks/not-a-number", wantStatus: http.StatusBadRequest},
		{name: "zero id", path: "/api/tasks/0", wantStatus: http.StatusBadRequest},
		{name: "not found", path: "/api/tasks/999", err: service.ErrTaskNotFound, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "internal error", path: "/api/tasks/1", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeTaskCreator{task: tt.task, err: tt.err}
			response := performRequest(t, creator, http.MethodGet, tt.path, "")

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
			if creator.calls != tt.wantCalls {
				t.Fatalf("service calls = %d, want %d", creator.calls, tt.wantCalls)
			}
		})
	}
}

func TestTaskHandlerDelete(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "success", wantStatus: http.StatusNoContent},
		{name: "not found", err: service.ErrTaskNotFound, wantStatus: http.StatusNotFound},
		{name: "active", err: service.ErrTaskConflict, wantStatus: http.StatusConflict},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeTaskCreator{err: tt.err}
			response := performRequest(t, creator, http.MethodDelete, "/api/tasks/7", "")
			if response.Code != tt.wantStatus || creator.deletedID != 7 {
				t.Fatalf("status=%d deletedID=%d", response.Code, creator.deletedID)
			}
		})
	}
}

func performCreateRequest(t *testing.T, creator TaskApplication, body string) *httptest.ResponseRecorder {
	t.Helper()
	return performRequest(t, creator, http.MethodPost, "/api/tasks", body)
}

func performRequest(t *testing.T, creator TaskApplication, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskHandler(creator)
	router.POST("/api/tasks", handler.Create)
	router.GET("/api/tasks", handler.List)
	router.GET("/api/tasks/:id", handler.GetByID)
	router.DELETE("/api/tasks/:id", handler.Delete)

	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
