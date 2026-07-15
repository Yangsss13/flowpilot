package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/checkpoint"
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
	interrupts   int
	taskStarts   int
}

func (f *fakeAgentStateStore) TransitionTask(_ context.Context, _ uint64, current, next domain.Status, _ domain.LogLevel, _ string) error {
	if err := domain.ValidateTransition(current, next); err != nil {
		return err
	}
	f.taskStatus = next
	f.taskStarts++
	return nil
}

func (f *fakeAgentStateStore) InterruptAgentTask(_ context.Context, _ uint64, _ uint64, observation agent.Observation, message string) error {
	f.interrupts++
	f.taskStatus = domain.StatusFailed
	f.taskResult = message
	if observation.StepID != "" || observation.Error != "" {
		f.observations = append(f.observations, observation)
	}
	return nil
}

type fakeCheckpointStore struct {
	value   checkpoint.Agent
	exists  bool
	saves   []checkpoint.Agent
	deletes int
}

func (f *fakeCheckpointStore) Save(_ context.Context, value checkpoint.Agent) error {
	f.value = value
	f.exists = true
	f.saves = append(f.saves, value)
	return nil
}

func (f *fakeCheckpointStore) Load(_ context.Context, _ uint64) (checkpoint.Agent, bool, error) {
	return f.value, f.exists, nil
}

func (f *fakeCheckpointStore) Delete(_ context.Context, _ uint64) error {
	f.exists = false
	f.deletes++
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
	checkpoints := &fakeCheckpointStore{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, tools, checkpoints)

	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if store.taskStatus != domain.StatusSuccess || store.taskResult != "final answer" || len(store.observations) != 2 {
		t.Fatalf("status=%s result=%q observations=%#v", store.taskStatus, store.taskResult, store.observations)
	}
	if !equalStrings(tools.calls, []string{"search", "summarize"}) {
		t.Fatalf("tool calls = %v", tools.calls)
	}
	if checkpoints.exists || checkpoints.deletes != 1 {
		t.Fatalf("checkpoint exists=%v deletes=%d", checkpoints.exists, checkpoints.deletes)
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
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{fail: map[string]error{"search": errors.New("Qdrant down")}}, &fakeCheckpointStore{})

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
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{}, &fakeCheckpointStore{})

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
	runner := NewAgentRunner(&fakeTaskSource{task: task}, store, planner, &fakeAgentTools{}, &fakeCheckpointStore{})

	err := runner.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrAgentExecution) || store.taskStatus != domain.StatusFailed {
		t.Fatalf("error=%v status=%s", err, store.taskStatus)
	}
}

func TestAgentRunnerOnlyFinishesWhenEveryStepSucceeded(t *testing.T) {
	tests := []struct {
		name       string
		lastStatus domain.Status
		wantErr    bool
	}{
		{name: "pending step", lastStatus: domain.StatusPending, wantErr: true},
		{name: "running step", lastStatus: domain.StatusRunning, wantErr: true},
		{name: "failed step", lastStatus: domain.StatusFailed, wantErr: true},
		{name: "all successful", lastStatus: domain.StatusSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := testAgentTask()
			task.Steps[0].Status = domain.StatusSuccess
			task.Steps[1].Status = tt.lastStatus
			planner := &fakeAgentPlanner{decisions: []agent.Decision{{Action: agent.DecisionFinish, FinalAnswer: "done"}}}
			states := &fakeAgentStateStore{}
			runner := NewAgentRunner(&fakeTaskSource{task: task}, states, planner, &fakeAgentTools{}, &fakeCheckpointStore{})

			err := runner.Execute(context.Background(), task.ID)
			if tt.wantErr {
				if !errors.Is(err, ErrAgentExecution) || states.taskStatus != domain.StatusFailed {
					t.Fatalf("error=%v status=%s", err, states.taskStatus)
				}
				return
			}
			if err != nil || states.taskStatus != domain.StatusSuccess {
				t.Fatalf("error=%v status=%s", err, states.taskStatus)
			}
		})
	}
}

func TestAgentRunnerResumesFromSafeCheckpoint(t *testing.T) {
	task := testAgentTask()
	task.Status = domain.StatusRunning
	task.Steps[0].Status = domain.StatusSuccess
	task.Steps[0].Observation = json.RawMessage(`{"step_id":"search","output":{"value":"search result"}}`)
	state, err := agentStateFromTask(task)
	if err != nil {
		t.Fatalf("agentStateFromTask() error = %v", err)
	}
	planner := &fakeAgentPlanner{decisions: []agent.Decision{
		{Action: agent.DecisionContinue, NextStepID: "summarize"},
		{Action: agent.DecisionFinish, FinalAnswer: "resumed"},
	}}
	states := &fakeAgentStateStore{taskStatus: domain.StatusRunning}
	checkpoints := &fakeCheckpointStore{exists: true, value: checkpoint.Agent{
		TaskID: task.ID, Phase: checkpoint.PhaseReady, NextIteration: 1, State: state,
	}}
	tools := &fakeAgentTools{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, states, planner, tools, checkpoints)

	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if states.taskStarts != 0 || states.taskStatus != domain.StatusSuccess || !equalStrings(tools.calls, []string{"summarize"}) {
		t.Fatalf("starts=%d status=%s calls=%v", states.taskStarts, states.taskStatus, tools.calls)
	}
}

func TestAgentRunnerFailsAmbiguousExecutingCheckpointWithoutReplaying(t *testing.T) {
	task := testAgentTask()
	task.Status = domain.StatusRunning
	task.Steps[0].Status = domain.StatusRunning
	states := &fakeAgentStateStore{taskStatus: domain.StatusRunning}
	checkpoints := &fakeCheckpointStore{exists: true, value: checkpoint.Agent{
		TaskID: task.ID, Phase: checkpoint.PhaseExecuting, CurrentStepID: task.Steps[0].ID,
	}}
	tools := &fakeAgentTools{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, states, &fakeAgentPlanner{}, tools, checkpoints)

	err := runner.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrAgentExecution) || states.interrupts != 1 || states.taskStatus != domain.StatusFailed {
		t.Fatalf("error=%v interrupts=%d status=%s", err, states.interrupts, states.taskStatus)
	}
	if len(tools.calls) != 0 {
		t.Fatalf("tool was replayed: %v", tools.calls)
	}
}

func TestAgentRunnerAdvancesStaleExecutingCheckpointAfterMySQLCommit(t *testing.T) {
	task := testAgentTask()
	task.Steps = task.Steps[:1]
	task.Status = domain.StatusRunning
	task.Steps[0].Status = domain.StatusSuccess
	task.Steps[0].Observation = json.RawMessage(`{"step_id":"search","output":{"value":"done"}}`)
	states := &fakeAgentStateStore{taskStatus: domain.StatusRunning}
	checkpoints := &fakeCheckpointStore{exists: true, value: checkpoint.Agent{
		TaskID: task.ID, Phase: checkpoint.PhaseExecuting, CurrentStepID: task.Steps[0].ID, NextIteration: 0,
	}}
	planner := &fakeAgentPlanner{decisions: []agent.Decision{{Action: agent.DecisionFinish, FinalAnswer: "done"}}}
	tools := &fakeAgentTools{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, states, planner, tools, checkpoints)

	if err := runner.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(checkpoints.saves) == 0 || checkpoints.saves[0].NextIteration != 1 {
		t.Fatalf("reconciled checkpoints = %#v", checkpoints.saves)
	}
	if len(tools.calls) != 0 {
		t.Fatalf("persisted tool was replayed: %v", tools.calls)
	}
}

func TestAgentRunnerRejectsExecutingCheckpointWithoutPersistedResult(t *testing.T) {
	task := testAgentTask()
	task.Status = domain.StatusRunning
	states := &fakeAgentStateStore{taskStatus: domain.StatusRunning}
	checkpoints := &fakeCheckpointStore{exists: true, value: checkpoint.Agent{
		TaskID: task.ID, Phase: checkpoint.PhaseExecuting, CurrentStepID: task.Steps[0].ID,
	}}
	tools := &fakeAgentTools{}
	runner := NewAgentRunner(&fakeTaskSource{task: task}, states, &fakeAgentPlanner{}, tools, checkpoints)

	err := runner.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrAgentExecution) || states.interrupts != 1 || len(tools.calls) != 0 {
		t.Fatalf("error=%v interrupts=%d calls=%v", err, states.interrupts, tools.calls)
	}
}

func testAgentTask() *domain.Task {
	return &domain.Task{
		ID: 1, Description: "goal", TaskType: domain.TaskTypeAgent, Status: domain.StatusQueued,
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
