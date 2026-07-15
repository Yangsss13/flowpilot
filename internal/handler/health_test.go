package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/agent"
)

func TestHealthAndReadyHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		checks     map[string]ReadinessCheck
		path       string
		wantStatus int
	}{
		{name: "liveness ignores dependencies", checks: map[string]ReadinessCheck{"mysql": func(context.Context) error { return errors.New("password=secret") }}, path: "/health", wantStatus: http.StatusOK},
		{name: "ready", checks: map[string]ReadinessCheck{"mysql": func(context.Context) error { return nil }}, path: "/ready", wantStatus: http.StatusOK},
		{name: "not ready", checks: map[string]ReadinessCheck{"mysql": func(context.Context) error { return errors.New("password=secret") }}, path: "/ready", wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHealthHandler(tt.checks)
			router := gin.New()
			router.GET("/health", handler.Health)
			router.GET("/ready", handler.Ready)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("readiness leaked internal error: %s", response.Body.String())
			}
		})
	}
}

func TestCapabilityHandlerReturnsEnabledTools(t *testing.T) {
	handler := NewCapabilityHandler(true, []agent.ToolDefinition{{Name: agent.ToolRAGQuery, Description: "search"}}, true)
	router := gin.New()
	router.GET("/api/capabilities", handler.Get)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"rag_query"`) || strings.Contains(response.Body.String(), "input_schema") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
