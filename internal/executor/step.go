package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yangsss13/flowpilot/internal/action"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type WorkflowSearcher interface {
	SearchAdvanced(ctx context.Context, query string, topK int, minScore float64) ([]rag.SearchResult, error)
}

type WorkflowSummarizer interface {
	Summarize(ctx context.Context, instruction string, evidence json.RawMessage) (string, error)
}

const MaxWorkflowSummaryEvidenceBytes = 128 << 10
const MaxWorkflowSummaryRunes = 20_000

type StepExecutor struct {
	searcher   WorkflowSearcher
	summarizer WorkflowSummarizer
}

func (e *StepExecutor) WithSummarizer(summarizer WorkflowSummarizer) *StepExecutor {
	e.summarizer = summarizer
	return e
}

func NewStepExecutor(searcher ...WorkflowSearcher) *StepExecutor {
	var configured WorkflowSearcher
	if len(searcher) > 0 {
		configured = searcher[0]
	}
	return &StepExecutor{searcher: configured}
}

func (e *StepExecutor) Execute(ctx context.Context, step domain.TaskStep) (json.RawMessage, error) {
	return e.ExecuteWithPrevious(ctx, step, nil)
}

func (e *StepExecutor) ExecuteWithPrevious(ctx context.Context, step domain.TaskStep, previous []domain.TaskStep) (json.RawMessage, error) {
	switch step.ActionType {
	case "sleep":
		return executeSleep(ctx, step.ActionPayload)
	case "http_mock":
		return executeHTTPMock(step.ActionPayload)
	case "shell_mock":
		return executeShellMock(step.ActionPayload)
	case "rag_query":
		return e.executeRAGQuery(ctx, step.ActionPayload)
	case "llm_summarize":
		return e.executeLLMSummarize(ctx, step.ActionPayload, previous)
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

func (e *StepExecutor) executeLLMSummarize(ctx context.Context, payload json.RawMessage, previous []domain.TaskStep) (json.RawMessage, error) {
	if e.summarizer == nil {
		return nil, fmt.Errorf("llm_summarize is not configured")
	}
	input, err := action.ParseLLMSummarize(payload)
	if err != nil {
		return nil, err
	}
	type evidenceStep struct {
		StepName string             `json:"step_name"`
		Query    string             `json:"query"`
		Results  []rag.SearchResult `json:"results"`
	}
	evidence := make([]evidenceStep, 0, len(previous))
	for _, step := range previous {
		if step.ActionType != "rag_query" || step.Status != domain.StatusSuccess || len(step.Observation) == 0 {
			continue
		}
		var observation struct {
			Query   string             `json:"query"`
			Results []rag.SearchResult `json:"results"`
		}
		if err := json.Unmarshal(step.Observation, &observation); err != nil {
			return nil, fmt.Errorf("decode evidence from step %d: %w", step.ID, err)
		}
		if len(observation.Results) == 0 {
			continue
		}
		evidence = append(evidence, evidenceStep{StepName: step.Name, Query: observation.Query, Results: observation.Results})
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("llm_summarize requires at least one successful rag_query observation")
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("encode workflow evidence: %w", err)
	}
	if len(evidenceJSON) > MaxWorkflowSummaryEvidenceBytes {
		return nil, fmt.Errorf("workflow summary evidence exceeds %d bytes", MaxWorkflowSummaryEvidenceBytes)
	}
	summary, err := e.summarizer.Summarize(ctx, input.Instruction, evidenceJSON)
	if err != nil {
		return nil, fmt.Errorf("summarize workflow evidence: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, fmt.Errorf("workflow summarizer returned an empty report")
	}
	if utf8.RuneCountInString(summary) > MaxWorkflowSummaryRunes {
		return nil, fmt.Errorf("workflow summary exceeds %d characters", MaxWorkflowSummaryRunes)
	}
	return json.Marshal(struct {
		Instruction   string `json:"instruction"`
		Summary       string `json:"summary"`
		EvidenceSteps int    `json:"evidence_steps"`
	}{Instruction: input.Instruction, Summary: summary, EvidenceSteps: len(evidence)})
}
