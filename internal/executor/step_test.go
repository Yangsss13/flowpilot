package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type fakeWorkflowSearcher struct {
	results  []rag.SearchResult
	err      error
	calls    int
	query    string
	topK     int
	minScore float64
}

func (f *fakeWorkflowSearcher) SearchAdvanced(_ context.Context, query string, topK int, minScore float64) ([]rag.SearchResult, error) {
	f.calls++
	f.query, f.topK, f.minScore = query, topK, minScore
	return f.results, f.err
}

func TestStepExecutorExecute(t *testing.T) {
	tests := []struct {
		name    string
		step    domain.TaskStep
		wantErr bool
	}{
		{name: "sleep succeeds", step: step("sleep", `{"duration_ms":1}`)},
		{name: "sleep rejects zero", step: step("sleep", `{"duration_ms":0}`), wantErr: true},
		{name: "http mock succeeds", step: step("http_mock", `{"status":200}`)},
		{name: "http mock fails", step: step("http_mock", `{"status":500}`), wantErr: true},
		{name: "http mock rejects invalid status", step: step("http_mock", `{"status":999}`), wantErr: true},
		{name: "shell mock succeeds", step: step("shell_mock", `{"exit_code":0}`)},
		{name: "shell mock fails", step: step("shell_mock", `{"exit_code":1}`), wantErr: true},
		{name: "unsupported action", step: step("real_shell", `{}`), wantErr: true},
		{name: "invalid JSON", step: step("sleep", `{`), wantErr: true},
	}

	executor := NewStepExecutor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(context.Background(), tt.step)
			if tt.wantErr && err == nil {
				t.Fatal("Execute() returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Execute() returned error: %v", err)
			}
		})
	}
}

func TestStepExecutorRAGQueryReturnsStructuredObservation(t *testing.T) {
	searcher := &fakeWorkflowSearcher{results: []rag.SearchResult{{
		DocumentID: "53", Source: "时间线.docx", Section: "项目经历", Page: 2, Text: "FlowPilot 使用 Go", Score: 0.91,
	}}}
	observation, err := NewStepExecutor(searcher).Execute(context.Background(), step("rag_query", `{"query":"项目技术栈","top_k":3,"min_score":0.5}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if searcher.query != "项目技术栈" || searcher.topK != 3 || searcher.minScore != 0.5 {
		t.Fatalf("search arguments = %q, %d, %v", searcher.query, searcher.topK, searcher.minScore)
	}
	var output struct {
		Query   string             `json:"query"`
		Results []rag.SearchResult `json:"results"`
	}
	if err := json.Unmarshal(observation, &output); err != nil || output.Query != "项目技术栈" || len(output.Results) != 1 || output.Results[0].Page != 2 {
		t.Fatalf("observation=%s decode error=%v", observation, err)
	}
}

func TestStepExecutorRAGFailureIsReturned(t *testing.T) {
	searchErr := errors.New("vector store unavailable")
	_, err := NewStepExecutor(&fakeWorkflowSearcher{err: searchErr}).Execute(context.Background(), step("rag_query", `{"query":"policy"}`))
	if !errors.Is(err, searchErr) {
		t.Fatalf("Execute() error = %v, want wrapped search error", err)
	}
}

func TestStepExecutorSleepHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, err := NewStepExecutor().Execute(ctx, step("sleep", `{"duration_ms":30000}`))
	if err == nil {
		t.Fatal("Execute() returned nil, want cancellation error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled sleep took %s, want under 1s", elapsed)
	}
}

func step(actionType, payload string) domain.TaskStep {
	return domain.TaskStep{
		ActionType:    actionType,
		ActionPayload: json.RawMessage(payload),
	}
}
