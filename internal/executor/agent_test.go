package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

type fakeAgentPlanner struct {
	decisions []agent.Decision
	replan    agent.Plan
	replans   int
}

func (f *fakeAgentPlanner) Decide(_ context.Context, _ agent.AgentState) (agent.Decision, error) {
	if len(f.decisions) == 0 {
		return agent.Decision{}, errors.New("no decision")
	}
	decision := f.decisions[0]
	f.decisions = f.decisions[1:]
	return decision, nil
}

func (f *fakeAgentPlanner) Replan(_ context.Context, _ agent.AgentState, _ string) (agent.Plan, error) {
	f.replans++
	return f.replan, nil
}

type fakeAgentTools struct {
	calls []string
	fail  map[string]error
}

func (f *fakeAgentTools) Execute(_ context.Context, _ agent.ToolName, input json.RawMessage) (json.RawMessage, error) {
	var value map[string]string
	_ = json.Unmarshal(input, &value)
	name := value["query"]
	f.calls = append(f.calls, name)
	if err := f.fail[name]; err != nil {
		return nil, err
	}
	return json.RawMessage(`{"value":"` + name + ` result"}`), nil
}

type fakeAgentStateStore struct {
	taskStatus   domain.Status
	taskResult   string
	observations []agent.Observation
	replanCount  int
	nextStepID   uint64
}

func (f *fakeAgentStateStore) TransitionTask(_ context.Context, _ uint64, current, next domain.Status, _ domain.LogLevel, _ string) error {
	if err := domain.ValidateTransition(current, next); err != nil {
		return err
	}
	f.taskStatus = next
	return nil
}

func (f *fakeAgentStateStore) TransitionStep(_ context.Context, _, _ uint64, current, next domain.Status, _ domain.LogLevel, _ string) error {
	return domain.ValidateTransition(current, next)
}

func (f *fakeAgentStateStore) CompleteAgentStep(_ context.Context, _, _ uint64, next domain.Status, observation agent.Observation) error {
	f.observations = append(f.observations, observation)
	return domain.ValidateTransition(domain.StatusRunning, next)
}

func (f *fakeAgentStateStore) CompleteAgentTask(_ context.Context, _ uint64, next domain.Status, result, _ string) error {
	f.taskStatus = next
	f.taskResult = result
	return domain.ValidateTransition(domain.StatusRunning, next)
}

func (f *fakeAgentStateStore) ReplaceAgentPlan(_ context.Context, taskID uint64, plan agent.Plan, replanCount int) ([]domain.TaskStep, error) {
	f.replanCount = replanCount
	steps := make([]domain.TaskStep, len(plan.Steps))
	for index, planStep := range plan.Steps {
		f.nextStepID++
		dependsOn, _ := json.Marshal(planStep.DependsOn)
		steps[index] = domain.TaskStep{ID: f.nextStepID, TaskID: taskID, Name: planStep.ID, ActionType: string(planStep.Tool), ActionPayload: planStep.Input, DependsOn: dependsOn, Status: domain.StatusPending}
	}
	return steps, nil
}

func TestAgentRunnerExecutesPlanAndFinishes(t *testing.T) {
	task := testAgentTask()
	planner := &fakeAgentPlanner{decisions: []agent.Decision{
		{Action: agent.DecisionContinue, NextStepID: "search"},
		{Action: agent.DecisionContinue, NextStepID: "summarize"},
		{Action: agent.DecisionFinish, FinalAnswer: "final answer"},
	}}
	tools := &fakeAgentTools{}
	store := &fakeAgentStateStore{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, tools)

	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if store.taskStatus != domain.StatusSuccess || store.taskResult != "final answer" || len(store.observations) != 2 {
		t.Fatalf("status=%s result=%q observations=%#v", store.taskStatus, store.taskResult, store.observations)
	}
	if !equalStrings(tools.calls, []string{"search", "summarize"}) {
		t.Fatalf("tool calls = %v", tools.calls)
	}
}

func TestAgentRunnerLetsModelHandleToolFailure(t *testing.T) {
	task := testAgentTask()
	task.Steps = task.Steps[:1]
	planner := &fakeAgentPlanner{decisions: []agent.Decision{
		{Action: agent.DecisionContinue, NextStepID: "search"},
		{Action: agent.DecisionFail, Reason: "knowledge unavailable"},
	}}
	store := &fakeAgentStateStore{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{fail: map[string]error{"search": errors.New("Qdrant down")}})

	err := runner.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrAgentExecution) || store.taskStatus != domain.StatusFailed || len(store.observations) != 1 || store.observations[0].Error == "" {
		t.Fatalf("error=%v status=%s observations=%#v", err, store.taskStatus, store.observations)
	}
}

func TestAgentRunnerReplansAndExecutesReplacement(t *testing.T) {
	task := testAgentTask()
	planner := &fakeAgentPlanner{
		decisions: []agent.Decision{
			{Action: agent.DecisionReplan, Reason: "use another query"},
			{Action: agent.DecisionContinue, NextStepID: "replacement"},
			{Action: agent.DecisionFinish, FinalAnswer: "done"},
		},
		replan: agent.Plan{Steps: []agent.PlanStep{{
			ID: "replacement", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"replacement"}`),
		}}},
	}
	store := &fakeAgentStateStore{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{})

	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if planner.replans != 1 || store.replanCount != 1 || store.taskStatus != domain.StatusSuccess {
		t.Fatalf("replans=%d persisted=%d status=%s", planner.replans, store.replanCount, store.taskStatus)
	}
}

func TestAgentRunnerRejectsUnsatisfiedDependency(t *testing.T) {
	task := testAgentTask()
	planner := &fakeAgentPlanner{decisions: []agent.Decision{{Action: agent.DecisionContinue, NextStepID: "summarize"}}}
	store := &fakeAgentStateStore{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{})

	err := runner.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrAgentExecution) || store.taskStatus != domain.StatusFailed {
		t.Fatalf("error=%v status=%s", err, store.taskStatus)
	}
}

func testAgentTask() *domain.Task {
	return &domain.Task{
		ID: 1, Description: "goal", TaskType: domain.TaskTypeAgent, Status: domain.StatusPending,
		Steps: []domain.TaskStep{
			{ID: 11, Name: "search", ActionType: string(agent.ToolRAGQuery), ActionPayload: json.RawMessage(`{"query":"search"}`), DependsOn: json.RawMessage(`[]`), Status: domain.StatusPending},
			{ID: 12, Name: "summarize", ActionType: string(agent.ToolRAGQuery), ActionPayload: json.RawMessage(`{"query":"summarize"}`), DependsOn: json.RawMessage(`["search"]`), Status: domain.StatusPending},
		},
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
