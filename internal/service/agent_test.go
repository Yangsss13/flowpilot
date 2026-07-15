package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

type fakeAgentPlanner struct {
	plan  agent.Plan
	err   error
	goal  string
	calls int
}

func (f *fakeAgentPlanner) CreatePlan(_ context.Context, goal string) (agent.Plan, error) {
	f.calls++
	f.goal = goal
	return f.plan, f.err
}

func TestAgentServiceCreatesTaskFromValidatedPlan(t *testing.T) {
	planner := &fakeAgentPlanner{plan: agent.Plan{Steps: []agent.PlanStep{
		{ID: "search", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"refund"}`)},
		{ID: "fetch", Tool: agent.ToolHTTPRequest, Input: json.RawMessage(`{"method":"GET","url":"https://example.com"}`), DependsOn: []string{"search"}},
	}}}
	repository := &fakeTaskRepository{}
	service := NewAgentService(planner, repository)

	task, err := service.Create(context.Background(), CreateAgentTaskInput{Goal: "  summarize refund policy  "})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if planner.calls != 1 || planner.goal != "summarize refund policy" {
		t.Fatalf("planner calls=%d goal=%q", planner.calls, planner.goal)
	}
	if repository.calls != 1 || repository.created != task {
		t.Fatalf("repository calls=%d created=%p task=%p", repository.calls, repository.created, task)
	}
	if task.TaskType != domain.TaskTypeAgent || task.Status != domain.StatusPending || task.Name != "summarize refund policy" || task.Description != planner.goal {
		t.Fatalf("unexpected task: %#v", task)
	}
	if len(task.Steps) != 2 {
		t.Fatalf("steps length = %d, want 2", len(task.Steps))
	}
	if task.Steps[0].StepOrder != 1 || task.Steps[0].ActionType != string(agent.ToolRAGQuery) || string(task.Steps[0].DependsOn) != `[]` {
		t.Fatalf("unexpected first step: %#v", task.Steps[0])
	}
	if task.Steps[1].StepOrder != 2 || task.Steps[1].ActionType != string(agent.ToolHTTPRequest) || string(task.Steps[1].DependsOn) != `["search"]` {
		t.Fatalf("unexpected second step: %#v", task.Steps[1])
	}
}

func TestAgentServiceValidatesInputBeforePlanning(t *testing.T) {
	tests := []struct {
		name  string
		input CreateAgentTaskInput
	}{
		{name: "empty goal", input: CreateAgentTaskInput{Goal: "  "}},
		{name: "long goal", input: CreateAgentTaskInput{Goal: strings.Repeat("a", maxAgentGoalRunes+1)}},
		{name: "long name", input: CreateAgentTaskInput{Name: strings.Repeat("a", maxTaskNameRunes+1), Goal: "goal"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &fakeAgentPlanner{}
			repository := &fakeTaskRepository{}
			service := NewAgentService(planner, repository)

			_, err := service.Create(context.Background(), test.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
			}
			if planner.calls != 0 || repository.calls != 0 {
				t.Fatalf("invalid input reached dependencies: planner=%d repository=%d", planner.calls, repository.calls)
			}
		})
	}
}

func TestAgentServiceDoesNotPersistPlannerFailure(t *testing.T) {
	plannerErr := errors.New("model unavailable")
	planner := &fakeAgentPlanner{err: plannerErr}
	repository := &fakeTaskRepository{}
	service := NewAgentService(planner, repository)

	_, err := service.Create(context.Background(), CreateAgentTaskInput{Goal: "goal"})
	if !errors.Is(err, ErrAgentPlanGeneration) || !errors.Is(err, plannerErr) {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
}

func TestAgentServiceWrapsRepositoryFailure(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	planner := &fakeAgentPlanner{plan: agent.Plan{Steps: []agent.PlanStep{{
		ID: "search", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"goal"}`),
	}}}}
	service := NewAgentService(planner, &fakeTaskRepository{err: repositoryErr})

	_, err := service.Create(context.Background(), CreateAgentTaskInput{Name: "task", Goal: "goal"})
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("Create() error = %v, want repository error", err)
	}
}
