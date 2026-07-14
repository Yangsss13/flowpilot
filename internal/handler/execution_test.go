package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type fakeExecutionApplication struct {
	logs []domain.ExecutionLog
	err  error
}

func (f *fakeExecutionApplication) Submit(_ context.Context, _ uint64) error {
	return f.err
}

func (f *fakeExecutionApplication) Logs(_ context.Context, _ uint64) ([]domain.ExecutionLog, error) {
	return f.logs, f.err
}

func TestExecutionHandlerRunMapsResults(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		app        *fakeExecutionApplication
		wantStatus int
	}{
		{name: "accepted", path: "/api/tasks/1/run", app: &fakeExecutionApplication{}, wantStatus: http.StatusAccepted},
		{name: "invalid id", path: "/api/tasks/nope/run", app: &fakeExecutionApplication{}, wantStatus: http.StatusBadRequest},
		{name: "not found", path: "/api/tasks/1/run", app: &fakeExecutionApplication{err: service.ErrTaskNotFound}, wantStatus: http.StatusNotFound},
		{name: "conflict", path: "/api/tasks/1/run", app: &fakeExecutionApplication{err: service.ErrTaskConflict}, wantStatus: http.StatusConflict},
		{name: "queue unavailable", path: "/api/tasks/1/run", app: &fakeExecutionApplication{err: service.ErrQueueUnavailable}, wantStatus: http.StatusServiceUnavailable},
		{name: "internal", path: "/api/tasks/1/run", app: &fakeExecutionApplication{err: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performExecutionRequest(tt.app, http.MethodPost, tt.path)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestExecutionHandlerLogs(t *testing.T) {
	app := &fakeExecutionApplication{logs: []domain.ExecutionLog{{ID: 1, TaskID: 1, Message: "task started"}}}
	response := performExecutionRequest(app, http.MethodGet, "/api/tasks/1/logs")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func performExecutionRequest(app ExecutionApplication, method, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewExecutionHandler(app)
	router.POST("/api/tasks/:id/run", handler.Run)
	router.GET("/api/tasks/:id/logs", handler.Logs)

	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
