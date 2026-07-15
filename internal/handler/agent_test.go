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

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type fakeAgentApplication struct {
	input service.CreateAgentTaskInput
	task  *domain.Task
	err   error
	calls int
}

func (f *fakeAgentApplication) Create(_ context.Context, input service.CreateAgentTaskInput) (*domain.Task, error) {
	f.calls++
	f.input = input
	return f.task, f.err
}

func TestAgentHandlerCreateReturns201(t *testing.T) {
	application := &fakeAgentApplication{task: &domain.Task{ID: 1, TaskType: domain.TaskTypeAgent, Status: domain.StatusPending}}
	response := performAgentCreateRequest(application, `{"name":"policy","goal":"summarize policy"}`)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", response.Code, response.Body.String())
	}
	if application.calls != 1 || application.input.Name != "policy" || application.input.Goal != "summarize policy" {
		t.Fatalf("unexpected application call: calls=%d input=%#v", application.calls, application.input)
	}
}

func TestAgentHandlerRejectsMalformedJSON(t *testing.T) {
	application := &fakeAgentApplication{}
	response := performAgentCreateRequest(application, `{"goal":`)

	if response.Code != http.StatusBadRequest || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, application.calls)
	}
}

func TestAgentHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{name: "invalid input", err: service.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantError: "invalid input"},
		{name: "provider failure", err: service.ErrAgentPlanGeneration, wantStatus: http.StatusBadGateway, wantError: "AI provider failed to generate a valid plan"},
		{name: "database failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantError: "internal server error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performAgentCreateRequest(&fakeAgentApplication{err: test.err}, `{"goal":"goal"}`)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error"] != test.wantError {
				t.Fatalf("error = %q, want %q", body["error"], test.wantError)
			}
		})
	}
}

func performAgentCreateRequest(application AgentApplication, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/agent/tasks", NewAgentHandler(application).Create)
	request := httptest.NewRequest(http.MethodPost, "/api/agent/tasks", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
