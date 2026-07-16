package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeChatProvider struct {
	plan        Plan
	planErr     error
	decision    Decision
	decisionErr error
	decisions   []Decision
	states      []AgentState
	request     PlanRequest
	tools       []ToolDefinition
}

func (f *fakeChatProvider) Plan(_ context.Context, request PlanRequest, tools []ToolDefinition) (Plan, error) {
	f.request = request
	f.tools = tools
	return f.plan, f.planErr
}

func (f *fakeChatProvider) Decide(_ context.Context, state AgentState) (Decision, error) {
	f.states = append(f.states, state)
	if len(f.decisions) > 0 {
		decision := f.decisions[0]
		f.decisions = f.decisions[1:]
		return decision, nil
	}
	return f.decision, f.decisionErr
}

func TestPlannerCreatesValidatedPlan(t *testing.T) {
	provider := &fakeChatProvider{plan: Plan{Steps: []PlanStep{
		{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy"}`)},
	}}}
	tools := DefaultToolDefinitions()
	planner := NewPlanner(provider, tools, newTestValidator(t))

	plan, err := planner.CreatePlan(context.Background(), "  summarize policy  ")
	if err != nil {
		t.Fatalf("CreatePlan() returned error: %v", err)
	}
	if len(plan.Steps) != 1 || provider.request.Goal != "summarize policy" || len(provider.tools) != len(tools) {
		t.Fatalf("unexpected plan or provider input: plan=%#v request=%#v tools=%d", plan, provider.request, len(provider.tools))
	}
}

func TestPlannerRejectsProviderAndValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		provider *fakeChatProvider
		wantErr  error
	}{
		{name: "provider", provider: &fakeChatProvider{planErr: errors.New("model unavailable")}},
		{name: "invalid plan", provider: &fakeChatProvider{plan: Plan{Steps: []PlanStep{{ID: "step-1", Tool: "shell", Input: json.RawMessage(`{}`)}}}}, wantErr: ErrInvalidPlan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner := NewPlanner(tt.provider, DefaultToolDefinitions(), newTestValidator(t))
			_, err := planner.CreatePlan(context.Background(), "goal")
			if err == nil {
				t.Fatal("CreatePlan() returned nil error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreatePlan() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPlannerValidatesDecision(t *testing.T) {
	provider := &fakeChatProvider{decision: Decision{Action: DecisionFinish, FinalAnswer: "answer"}}
	planner := NewPlanner(provider, DefaultToolDefinitions(), newTestValidator(t))
	decision, err := planner.Decide(context.Background(), AgentState{})
	if err != nil {
		t.Fatalf("Decide() returned error: %v", err)
	}
	if decision.Action != DecisionFinish {
		t.Fatalf("decision action = %q, want finish", decision.Action)
	}
}

func TestPlannerRepairsInvalidDecisionOnce(t *testing.T) {
	provider := &fakeChatProvider{decisions: []Decision{
		{Action: DecisionContinue, NextStepID: "step-1"},
		{Action: DecisionFinish, FinalAnswer: "grounded answer"},
	}}
	planner := NewPlanner(provider, DefaultToolDefinitions(), newTestValidator(t))
	state := AgentState{
		Plan:         Plan{Steps: []PlanStep{{ID: "step-1"}}},
		Observations: []Observation{{StepID: "step-1", Output: json.RawMessage(`{"ok":true}`)}},
	}
	decision, err := planner.Decide(context.Background(), state)
	if err != nil {
		t.Fatalf("Decide() returned error: %v", err)
	}
	if decision.Action != DecisionFinish || len(provider.states) != 2 || provider.states[1].DecisionFeedback == "" {
		t.Fatalf("decision=%#v states=%#v", decision, provider.states)
	}
}

func TestPlannerDoesNotRetryTransportDecisionError(t *testing.T) {
	provider := &fakeChatProvider{decisionErr: errors.New("model unavailable")}
	planner := NewPlanner(provider, DefaultToolDefinitions(), newTestValidator(t))
	if _, err := planner.Decide(context.Background(), AgentState{}); err == nil {
		t.Fatal("Decide() returned nil error")
	}
	if len(provider.states) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.states))
	}
}

func TestPlannerReplanIncludesPreviousState(t *testing.T) {
	provider := &fakeChatProvider{plan: Plan{Steps: []PlanStep{{
		ID: "replacement", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"new"}`),
	}}}}
	planner := NewPlanner(provider, DefaultToolDefinitions(), newTestValidator(t))
	state := AgentState{
		Goal:         "goal",
		Plan:         Plan{Steps: []PlanStep{{ID: "old", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"old"}`)}}},
		Observations: []Observation{{StepID: "old", Error: "not found"}},
		ReplanCount:  1,
	}
	plan, err := planner.Replan(context.Background(), state, "try another query")
	if err != nil {
		t.Fatalf("Replan() returned error: %v", err)
	}
	if plan.Steps[0].ID != "replacement" || provider.request.PreviousPlan == nil || provider.request.ReplanCount != 2 || provider.request.ReplanReason == "" {
		t.Fatalf("plan=%#v request=%#v", plan, provider.request)
	}
	state.ReplanCount = MaxReplans
	if _, err := planner.Replan(context.Background(), state, "again"); err == nil {
		t.Fatal("Replan() accepted exhausted limit")
	}
}
