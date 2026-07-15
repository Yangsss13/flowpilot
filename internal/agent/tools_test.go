package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/rag"
)

type fakeRAGSearcher struct {
	query   string
	results []rag.SearchResult
	err     error
}

func (f *fakeRAGSearcher) Search(_ context.Context, query string, _ int) ([]rag.SearchResult, error) {
	f.query = query
	return f.results, f.err
}

func TestToolExecutorRunsRAGQuery(t *testing.T) {
	searcher := &fakeRAGSearcher{results: []rag.SearchResult{{Source: "policy.md", Text: "seven days"}}}
	executor, err := NewToolExecutor(searcher, nil, nil)
	if err != nil {
		t.Fatalf("NewToolExecutor() returned error: %v", err)
	}
	output, err := executor.Execute(context.Background(), ToolRAGQuery, json.RawMessage(`{"query":"refund"}`))
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if searcher.query != "refund" || !strings.Contains(string(output), "policy.md") {
		t.Fatalf("query=%q output=%s", searcher.query, output)
	}
}

func TestToolExecutorRestrictsHTTPHostsAndRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(writer, request, "https://not-allowed.example/path", http.StatusFound)
			return
		}
		_, _ = writer.Write([]byte("ok"))
	}))
	defer target.Close()
	executor, err := NewToolExecutor(nil, []string{"127.0.0.1"}, target.Client())
	if err != nil {
		t.Fatalf("NewToolExecutor() returned error: %v", err)
	}
	input := json.RawMessage(`{"method":"GET","url":"` + target.URL + `/ok"}`)
	output, err := executor.Execute(context.Background(), ToolHTTPRequest, input)
	if err != nil || !strings.Contains(string(output), `"status_code":200`) {
		t.Fatalf("Execute() output=%s error=%v", output, err)
	}
	_, err = executor.Execute(context.Background(), ToolHTTPRequest, json.RawMessage(`{"method":"GET","url":"https://example.com"}`))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("disallowed host error = %v", err)
	}
	redirectInput := json.RawMessage(`{"method":"GET","url":"` + target.URL + `/redirect"}`)
	_, err = executor.Execute(context.Background(), ToolHTTPRequest, redirectInput)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestNewToolExecutorValidatesAllowedHosts(t *testing.T) {
	if _, err := NewToolExecutor(nil, []string{"https://example.com"}, nil); err == nil {
		t.Fatal("NewToolExecutor() accepted host with scheme")
	}
}
