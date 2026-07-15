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
	goal        string
	tools       []ToolDefinition
}

func (f *fakeChatProvider) Plan(_ context.Context, goal string, tools []ToolDefinition) (Plan, error) {
	f.goal = goal
	f.tools = tools
	return f.plan, f.planErr
}

func (f *fakeChatProvider) Decide(_ context.Context, _ AgentState) (Decision, error) {
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
	if len(plan.Steps) != 1 || provider.goal != "summarize policy" || len(provider.tools) != len(tools) {
		t.Fatalf("unexpected plan or provider input: plan=%#v goal=%q tools=%d", plan, provider.goal, len(provider.tools))
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
