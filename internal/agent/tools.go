package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Yangsss13/flowpilot/internal/rag"
)

const maxToolResponseBytes = 1 << 20

type RAGSearcher interface {
	Search(ctx context.Context, query string, topK int) ([]rag.SearchResult, error)
}

type ToolExecutor struct {
	rag          RAGSearcher
	allowedHosts map[string]struct{}
	httpClient   *http.Client
}

// ToolRegistry builds the advertised definitions and runtime executor from
// the same configured capabilities, so the Planner cannot select a disabled
// tool that will only fail later during execution.
type ToolRegistry struct {
	definitions []ToolDefinition
	executor    *ToolExecutor
}

func NewToolRegistry(searcher RAGSearcher, allowedHosts []string, client *http.Client) (*ToolRegistry, error) {
	executor, err := NewToolExecutor(searcher, allowedHosts, client)
	if err != nil {
		return nil, err
	}
	available := make(map[ToolName]bool, 2)
	available[ToolRAGQuery] = searcher != nil
	available[ToolHTTPRequest] = len(executor.allowedHosts) != 0
	definitions := make([]ToolDefinition, 0, len(available))
	for _, definition := range DefaultToolDefinitions() {
		if available[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	return &ToolRegistry{definitions: definitions, executor: executor}, nil
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	definitions := make([]ToolDefinition, len(r.definitions))
	copy(definitions, r.definitions)
	return definitions
}

func (r *ToolRegistry) Execute(ctx context.Context, tool ToolName, input json.RawMessage) (json.RawMessage, error) {
	return r.executor.Execute(ctx, tool, input)
}

func NewToolExecutor(searcher RAGSearcher, allowedHosts []string, client *http.Client) (*ToolExecutor, error) {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if strings.ContainsAny(host, "/:@") {
			return nil, fmt.Errorf("allowed HTTP host %q must be a hostname without scheme or port", host)
		}
		allowed[host] = struct{}{}
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	} else {
		clone := *client
		client = &clone
	}
	executor := &ToolExecutor{rag: searcher, allowedHosts: allowed, httpClient: client}
	client.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		return executor.checkAllowedURL(request.URL)
	}
	return executor, nil
}

func (e *ToolExecutor) Execute(ctx context.Context, tool ToolName, input json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case ToolRAGQuery:
		return e.executeRAG(ctx, input)
	case ToolHTTPRequest:
		return e.executeHTTP(ctx, input)
	default:
		return nil, fmt.Errorf("unsupported agent tool %q", tool)
	}
}

func (e *ToolExecutor) executeRAG(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if e.rag == nil {
		return nil, fmt.Errorf("RAG tool is not configured")
	}
	var request struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, fmt.Errorf("decode RAG input: %w", err)
	}
	results, err := e.rag.Search(ctx, request.Query, rag.DefaultTopK)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	output, err := json.Marshal(struct {
		Results []rag.SearchResult `json:"results"`
	}{Results: results})
	if err != nil {
		return nil, fmt.Errorf("encode RAG output: %w", err)
	}
	return output, nil
}

func (e *ToolExecutor) executeHTTP(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var value struct {
		Method string          `json:"method"`
		URL    string          `json:"url"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, fmt.Errorf("decode HTTP input: %w", err)
	}
	target, err := url.Parse(value.URL)
	if err != nil {
		return nil, fmt.Errorf("parse HTTP URL: %w", err)
	}
	if err := e.checkAllowedURL(target); err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(value.Method))
	if method != http.MethodGet && method != http.MethodPost {
		return nil, fmt.Errorf("HTTP tool method must be GET or POST")
	}
	var body io.Reader
	if method == http.MethodPost && len(value.Body) > 0 {
		body = bytes.NewReader(value.Body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create HTTP tool request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := e.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send HTTP tool request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxToolResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read HTTP tool response: %w", err)
	}
	if len(responseBody) > maxToolResponseBytes {
		return nil, fmt.Errorf("HTTP tool response exceeds %d bytes", maxToolResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP tool returned status %d", response.StatusCode)
	}
	output, err := json.Marshal(struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}{StatusCode: response.StatusCode, Body: string(responseBody)})
	if err != nil {
		return nil, fmt.Errorf("encode HTTP tool output: %w", err)
	}
	return output, nil
}

func (e *ToolExecutor) checkAllowedURL(target *url.URL) error {
	if target.Scheme != "http" && target.Scheme != "https" {
		return fmt.Errorf("HTTP tool URL must use http or https")
	}
	host := strings.ToLower(target.Hostname())
	if _, allowed := e.allowedHosts[host]; !allowed {
		return fmt.Errorf("HTTP tool host %q is not allowed", host)
	}
	if target.User != nil {
		return fmt.Errorf("HTTP tool URL must not contain credentials")
	}
	return nil
}
