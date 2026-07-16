package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleProviderPlan(t *testing.T) {
	var received chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeChatResponse(t, writer, `{"steps":[{"id":"search","tool":"rag_query","input":{"query":"refund policy"},"depends_on":[]}]}`)
	}))
	defer server.Close()

	provider := newTestOpenAICompatibleProvider(t, server.URL+"/v1", server.Client())
	plan, err := provider.Plan(context.Background(), PlanRequest{Goal: "summarize refund policy"}, DefaultToolDefinitions())
	if err != nil {
		t.Fatalf("Plan() returned error: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Tool != ToolRAGQuery {
		t.Fatalf("Plan() = %#v", plan)
	}
	if received.Model != "test-model" || received.ResponseFormat.Type != "json_object" || received.Temperature != 0 {
		t.Fatalf("unexpected request options: %#v", received)
	}
	if len(received.Messages) != 2 || !strings.Contains(received.Messages[1].Content, "summarize refund policy") || !strings.Contains(received.Messages[1].Content, "rag_query") {
		t.Fatalf("unexpected messages: %#v", received.Messages)
	}
}

func TestOpenAICompatibleProviderDecide(t *testing.T) {
	var received chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeChatResponse(t, writer, `{"action":"finish","final_answer":"done"}`)
	}))
	defer server.Close()

	provider := newTestOpenAICompatibleProvider(t, server.URL, server.Client())
	decision, err := provider.Decide(context.Background(), AgentState{Goal: "goal", DecisionFeedback: "step already succeeded"})
	if err != nil {
		t.Fatalf("Decide() returned error: %v", err)
	}
	if decision.Action != DecisionFinish || decision.FinalAnswer != "done" {
		t.Fatalf("Decide() = %#v", decision)
	}
	if len(received.Messages) != 2 || !strings.Contains(received.Messages[0].Content, "Never continue a succeeded step") || !strings.Contains(received.Messages[1].Content, "decision_feedback") {
		t.Fatalf("unexpected decision messages: %#v", received.Messages)
	}
}

func TestOpenAICompatibleProviderRejectsInvalidModelContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeChatResponse(t, writer, "not JSON")
	}))
	defer server.Close()

	provider := newTestOpenAICompatibleProvider(t, server.URL, server.Client())
	_, err := provider.Plan(context.Background(), PlanRequest{Goal: "goal"}, DefaultToolDefinitions())
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("Plan() error = %v, want ErrInvalidPlan", err)
	}
}

func TestOpenAICompatibleProviderHandlesHTTPAndEmptyResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "HTTP error", statusCode: http.StatusUnauthorized, body: `{"error":{"message":"test-key is invalid"}}`, want: "HTTP status 401"},
		{name: "malformed envelope", statusCode: http.StatusOK, body: `{`, want: "decode chat completion response"},
		{name: "no choices", statusCode: http.StatusOK, body: `{"choices":[]}`, want: "has no choices"},
		{name: "empty content", statusCode: http.StatusOK, body: `{"choices":[{"message":{"content":"  "}}]}`, want: "content is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := newTestOpenAICompatibleProvider(t, server.URL, server.Client())
			_, err := provider.Plan(context.Background(), PlanRequest{Goal: "goal"}, DefaultToolDefinitions())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Plan() error = %v, want substring %q", err, test.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Fatalf("Plan() error leaked API key: %v", err)
			}
		})
	}
}

func TestOpenAICompatibleProviderHonorsContextCancellation(t *testing.T) {
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-releaseHandler
	}))
	defer server.Close()
	defer close(releaseHandler)
	provider := newTestOpenAICompatibleProvider(t, server.URL, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := provider.Plan(ctx, PlanRequest{Goal: "goal"}, DefaultToolDefinitions())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Plan() error = %v, want context deadline exceeded", err)
	}
}

func TestNewOpenAICompatibleProviderValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		apiKey  string
		model   string
	}{
		{name: "missing URL", apiKey: "key", model: "model"},
		{name: "invalid URL", baseURL: "localhost:8080", apiKey: "key", model: "model"},
		{name: "credentials in URL", baseURL: "https://user:pass@example.com/v1", apiKey: "key", model: "model"},
		{name: "missing key", baseURL: "https://example.com/v1", model: "model"},
		{name: "missing model", baseURL: "https://example.com/v1", apiKey: "key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewOpenAICompatibleProvider(test.baseURL, test.apiKey, test.model, nil); err == nil {
				t.Fatal("NewOpenAICompatibleProvider() returned nil error")
			}
		})
	}
}

func newTestOpenAICompatibleProvider(t *testing.T, baseURL string, client *http.Client) *OpenAICompatibleProvider {
	t.Helper()
	provider, err := NewOpenAICompatibleProvider(baseURL, "test-key", "test-model", client)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() returned error: %v", err)
	}
	return provider
}

func writeChatResponse(t *testing.T, writer http.ResponseWriter, content string) {
	t.Helper()
	if err := json.NewEncoder(writer).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
	}); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
