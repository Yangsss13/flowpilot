package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Yangsss13/flowpilot/internal/action"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type WorkflowSearcher interface {
	SearchAdvanced(ctx context.Context, query string, topK int, minScore float64) ([]rag.SearchResult, error)
}

type StepExecutor struct {
	searcher WorkflowSearcher
}

func NewStepExecutor(searcher ...WorkflowSearcher) *StepExecutor {
	var configured WorkflowSearcher
	if len(searcher) > 0 {
		configured = searcher[0]
	}
	return &StepExecutor{searcher: configured}
}

func (e *StepExecutor) Execute(ctx context.Context, step domain.TaskStep) (json.RawMessage, error) {
	switch step.ActionType {
	case "sleep":
		return executeSleep(ctx, step.ActionPayload)
	case "http_mock":
		return executeHTTPMock(step.ActionPayload)
	case "shell_mock":
		return executeShellMock(step.ActionPayload)
	case "rag_query":
		return e.executeRAGQuery(ctx, step.ActionPayload)
	default:
		return nil, fmt.Errorf("unsupported action type %q", step.ActionType)
	}
}

func executeSleep(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	input, err := action.ParseSleep(payload)
	if err != nil {
		return nil, err
	}
	duration := time.Duration(input.DurationMS) * time.Millisecond

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return json.Marshal(struct {
			DurationMS int `json:"duration_ms"`
		}{DurationMS: input.DurationMS})
	case <-ctx.Done():
		return nil, fmt.Errorf("sleep interrupted: %w", ctx.Err())
	}
}

func executeHTTPMock(payload json.RawMessage) (json.RawMessage, error) {
	input, err := action.ParseHTTPMock(payload)
	if err != nil {
		return nil, err
	}
	if input.Status >= 400 {
		return nil, fmt.Errorf("http_mock returned status %d", input.Status)
	}
	return json.Marshal(struct {
		StatusCode int `json:"status_code"`
	}{StatusCode: input.Status})
}

func executeShellMock(payload json.RawMessage) (json.RawMessage, error) {
	input, err := action.ParseShellMock(payload)
	if err != nil {
		return nil, err
	}
	if input.ExitCode != 0 {
		return nil, fmt.Errorf("shell_mock exited with code %d", input.ExitCode)
	}
	return json.Marshal(struct {
		ExitCode int `json:"exit_code"`
	}{ExitCode: input.ExitCode})
}

func (e *StepExecutor) executeRAGQuery(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if e.searcher == nil {
		return nil, fmt.Errorf("rag_query is not configured")
	}
	input, err := action.ParseRAGQuery(payload)
	if err != nil {
		return nil, err
	}
	results, err := e.searcher.SearchAdvanced(ctx, input.Query, input.TopK, input.MinScore)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	return json.Marshal(struct {
		Query   string             `json:"query"`
		Results []rag.SearchResult `json:"results"`
	}{Query: input.Query, Results: results})
}
