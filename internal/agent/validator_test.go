package agent

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidatorAcceptsValidPlan(t *testing.T) {
	validator := newTestValidator(t)
	plan := Plan{Steps: []PlanStep{
		{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"refund policy"}`)},
		{ID: "step-2", Tool: ToolHTTPRequest, Input: json.RawMessage(`{"method":"POST","url":"https://example.com/report","body":{"source":"step-1"}}`), DependsOn: []string{"step-1"}},
	}}
	if err := validator.ValidatePlan(plan); err != nil {
		t.Fatalf("ValidatePlan() returned error: %v", err)
	}
}

func TestValidatorRejectsInvalidPlans(t *testing.T) {
	validStep := func(id string) PlanStep {
		return PlanStep{ID: id, Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy"}`)}
	}
	tests := []struct {
		name string
		plan Plan
	}{
		{name: "empty", plan: Plan{}},
		{name: "invalid id", plan: Plan{Steps: []PlanStep{{ID: "bad id", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy"}`)}}}},
		{name: "duplicate id", plan: Plan{Steps: []PlanStep{validStep("step-1"), validStep("step-1")}}},
		{name: "unknown tool", plan: Plan{Steps: []PlanStep{{ID: "step-1", Tool: "shell", Input: json.RawMessage(`{}`)}}}},
		{name: "empty query", plan: Plan{Steps: []PlanStep{{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":""}`)}}}},
		{name: "extra input", plan: Plan{Steps: []PlanStep{{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy","extra":true}`)}}}},
		{name: "unknown dependency", plan: Plan{Steps: []PlanStep{{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy"}`), DependsOn: []string{"missing"}}}}},
		{name: "duplicate dependency", plan: Plan{Steps: []PlanStep{validStep("step-1"), {ID: "step-2", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"policy"}`), DependsOn: []string{"step-1", "step-1"}}}}},
		{name: "cycle", plan: Plan{Steps: []PlanStep{
			{ID: "step-1", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"one"}`), DependsOn: []string{"step-2"}},
			{ID: "step-2", Tool: ToolRAGQuery, Input: json.RawMessage(`{"query":"two"}`), DependsOn: []string{"step-1"}},
		}}},
	}
	tooMany := Plan{}
	for i := 0; i < MaxPlanSteps+1; i++ {
		tooMany.Steps = append(tooMany.Steps, validStep("step-"+string(rune('a'+i))))
	}
	tests = append(tests, struct {
		name string
		plan Plan
	}{name: "too many steps", plan: tooMany})

	validator := newTestValidator(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validator.ValidatePlan(tt.plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("ValidatePlan() error = %v, want ErrInvalidPlan", err)
			}
		})
	}
}

func TestParsePlanJSONIsStrict(t *testing.T) {
	valid := []byte(`{"steps":[{"id":"step-1","tool":"rag_query","input":{"query":"policy"},"depends_on":[]}]}`)
	plan, err := ParsePlanJSON(valid)
	if err != nil {
		t.Fatalf("ParsePlanJSON() returned error: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(plan.Steps))
	}

	invalidInputs := [][]byte{
		[]byte(`{"steps":`),
		[]byte(`{"steps":[],"unknown":true}`),
		[]byte(`{"steps":[]} {"steps":[]}`),
	}
	for _, input := range invalidInputs {
		if _, err := ParsePlanJSON(input); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("ParsePlanJSON(%s) error = %v, want ErrInvalidPlan", input, err)
		}
	}
}

func TestValidatorValidatesDecision(t *testing.T) {
	validator := newTestValidator(t)
	state := AgentState{
		Plan:         Plan{Steps: []PlanStep{{ID: "step-1"}}},
		Observations: []Observation{{StepID: "step-1", Output: json.RawMessage(`{"ok":true}`)}},
	}
	tests := []struct {
		name     string
		decision Decision
		state    AgentState
		wantErr  bool
	}{
		{name: "continue", decision: Decision{Action: DecisionContinue, NextStepID: "step-1"}, state: state},
		{name: "missing next step", decision: Decision{Action: DecisionContinue}, state: state, wantErr: true},
		{name: "finish", decision: Decision{Action: DecisionFinish, FinalAnswer: "done"}, state: state},
		{name: "empty answer", decision: Decision{Action: DecisionFinish}, state: state, wantErr: true},
		{name: "finish before step succeeds", decision: Decision{Action: DecisionFinish, FinalAnswer: "done"}, state: AgentState{Plan: state.Plan}, wantErr: true},
		{name: "finish after failed observation", decision: Decision{Action: DecisionFinish, FinalAnswer: "done"}, state: AgentState{Plan: state.Plan, Observations: []Observation{{StepID: "step-1", Error: "failed"}}}, wantErr: true},
		{name: "replan", decision: Decision{Action: DecisionReplan, Reason: "missing data"}, state: state},
		{name: "replan limit", decision: Decision{Action: DecisionReplan, Reason: "again"}, state: AgentState{ReplanCount: MaxReplans}, wantErr: true},
		{name: "fail", decision: Decision{Action: DecisionFail, Reason: "tool failed"}, state: state},
		{name: "unknown", decision: Decision{Action: "wait"}, state: state, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateDecision(tt.decision, tt.state)
			if tt.wantErr && !errors.Is(err, ErrInvalidDecision) {
				t.Fatalf("ValidateDecision() error = %v, want ErrInvalidDecision", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateDecision() returned error: %v", err)
			}
		})
	}
}

func newTestValidator(t *testing.T) *Validator {
	t.Helper()
	validator, err := NewValidator(DefaultToolDefinitions(), MaxPlanSteps)
	if err != nil {
		t.Fatalf("NewValidator() returned error: %v", err)
	}
	return validator
}
