package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

type fakeTaskSource struct {
	task *domain.Task
	err  error
}

func (f *fakeTaskSource) GetByID(_ context.Context, _ uint64) (*domain.Task, error) {
	return f.task, f.err
}

type transitionEvent struct {
	kind    string
	id      uint64
	current domain.Status
	next    domain.Status
}

type fakeExecutionStateStore struct {
	events       []transitionEvent
	observations map[uint64]json.RawMessage
	err          error
}

func (f *fakeExecutionStateStore) CompleteWorkflowStep(ctx context.Context, _ uint64, stepID uint64, observation json.RawMessage, _ domain.LogLevel, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.events = append(f.events, transitionEvent{kind: "step", id: stepID, current: domain.StatusRunning, next: domain.StatusSuccess})
	if f.err != nil {
		return f.err
	}
	if !json.Valid(observation) {
		return errors.New("invalid observation")
	}
	if f.observations == nil {
		f.observations = make(map[uint64]json.RawMessage)
	}
	f.observations[stepID] = append(json.RawMessage(nil), observation...)
	return nil
}

func (f *fakeExecutionStateStore) TransitionTask(ctx context.Context, taskID uint64, current, next domain.Status, _ domain.LogLevel, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.events = append(f.events, transitionEvent{kind: "task", id: taskID, current: current, next: next})
	if f.err != nil {
		return f.err
	}
	return domain.ValidateTransition(current, next)
}

func (f *fakeExecutionStateStore) TransitionStep(ctx context.Context, _ uint64, stepID uint64, current, next domain.Status, _ domain.LogLevel, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.events = append(f.events, transitionEvent{kind: "step", id: stepID, current: current, next: next})
	if f.err != nil {
		return f.err
	}
	return domain.ValidateTransition(current, next)
}

type fakeStepRunner struct {
	calls  []uint64
	failID uint64
}

type cancellingStepRunner struct {
	cancel context.CancelFunc
}

func (r *cancellingStepRunner) Execute(_ context.Context, _ domain.TaskStep) (json.RawMessage, error) {
	r.cancel()
	return nil, context.Canceled
}

func (f *fakeStepRunner) Execute(_ context.Context, step domain.TaskStep) (json.RawMessage, error) {
	f.calls = append(f.calls, step.ID)
	if step.ID == f.failID {
		return nil, errors.New("mock action failed")
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func TestTaskExecutorExecuteSuccess(t *testing.T) {
	task := pendingTask()
	states := &fakeExecutionStateStore{}
	steps := &fakeStepRunner{}
	executor := NewTaskExecutor(&fakeTaskSource{task: task}, states, steps)

	if err := executor.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	wantStepCalls := []uint64{11, 12, 13}
	if !equalIDs(steps.calls, wantStepCalls) {
		t.Fatalf("step calls = %v, want %v", steps.calls, wantStepCalls)
	}
	wantEvents := []transitionEvent{
		{kind: "task", id: 1, current: domain.StatusQueued, next: domain.StatusRunning},
		{kind: "step", id: 11, current: domain.StatusPending, next: domain.StatusRunning},
		{kind: "step", id: 11, current: domain.StatusRunning, next: domain.StatusSuccess},
		{kind: "step", id: 12, current: domain.StatusPending, next: domain.StatusRunning},
		{kind: "step", id: 12, current: domain.StatusRunning, next: domain.StatusSuccess},
		{kind: "step", id: 13, current: domain.StatusPending, next: domain.StatusRunning},
		{kind: "step", id: 13, current: domain.StatusRunning, next: domain.StatusSuccess},
		{kind: "task", id: 1, current: domain.StatusRunning, next: domain.StatusSuccess},
	}
	if !equalEvents(states.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", states.events, wantEvents)
	}
	if len(states.observations) != 3 || string(states.observations[12]) != `{"ok":true}` {
		t.Fatalf("observations = %#v, want persisted output for every step", states.observations)
	}
}

func TestTaskExecutorStopsAfterFirstFailedStep(t *testing.T) {
	task := pendingTask()
	states := &fakeExecutionStateStore{}
	steps := &fakeStepRunner{failID: 12}
	executor := NewTaskExecutor(&fakeTaskSource{task: task}, states, steps)

	if err := executor.Execute(context.Background(), task.ID); err == nil {
		t.Fatal("Execute() returned nil, want step failure")
	}

	wantStepCalls := []uint64{11, 12}
	if !equalIDs(steps.calls, wantStepCalls) {
		t.Fatalf("step calls = %v, want %v", steps.calls, wantStepCalls)
	}
	last := states.events[len(states.events)-1]
	if last.kind != "task" || last.next != domain.StatusFailed {
		t.Fatalf("last event = %#v, want task Failed", last)
	}
	for _, event := range states.events {
		if event.id == 13 {
			t.Fatalf("third step unexpectedly transitioned: %#v", event)
		}
	}
}

func TestTaskExecutorStopsAfterRAGFailure(t *testing.T) {
	task := pendingTask()
	task.Steps = []domain.TaskStep{
		{ID: 11, StepOrder: 1, Status: domain.StatusPending, ActionType: "rag_query", ActionPayload: json.RawMessage(`{"query":"项目架构"}`)},
		{ID: 12, StepOrder: 2, Status: domain.StatusPending, ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`)},
	}
	states := &fakeExecutionStateStore{}
	searcher := &fakeWorkflowSearcher{err: errors.New("knowledge unavailable")}
	executor := NewTaskExecutor(&fakeTaskSource{task: task}, states, NewStepExecutor(searcher))

	if err := executor.Execute(context.Background(), task.ID); err == nil {
		t.Fatal("Execute() returned nil, want RAG failure")
	}
	if searcher.calls != 1 {
		t.Fatalf("RAG calls = %d, want 1", searcher.calls)
	}
	for _, event := range states.events {
		if event.kind == "step" && event.id == 12 {
			t.Fatalf("step after failed RAG unexpectedly transitioned: %#v", event)
		}
	}
}

func TestTaskExecutorSkipsSuccessfulStepsWhenRetryingFailedTask(t *testing.T) {
	task := pendingTask()
	task.Status = domain.StatusQueued
	task.Steps[0].Status = domain.StatusSuccess
	task.Steps[1].Status = domain.StatusFailed
	states := &fakeExecutionStateStore{}
	steps := &fakeStepRunner{}
	executor := NewTaskExecutor(&fakeTaskSource{task: task}, states, steps)

	if err := executor.Execute(context.Background(), task.ID); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	wantStepCalls := []uint64{12, 13}
	if !equalIDs(steps.calls, wantStepCalls) {
		t.Fatalf("step calls = %v, want %v", steps.calls, wantStepCalls)
	}
}

func TestTaskExecutorPersistsFailureAfterContextCancellation(t *testing.T) {
	task := pendingTask()
	task.Steps = task.Steps[:1]
	states := &fakeExecutionStateStore{}
	ctx, cancel := context.WithCancel(context.Background())
	executor := NewTaskExecutor(
		&fakeTaskSource{task: task},
		states,
		&cancellingStepRunner{cancel: cancel},
	)

	if err := executor.Execute(ctx, task.ID); err == nil {
		t.Fatal("Execute() returned nil, want cancellation error")
	}

	last := states.events[len(states.events)-1]
	if last.kind != "task" || last.next != domain.StatusFailed {
		t.Fatalf("last event = %#v, want task Failed", last)
	}
}

func pendingTask() *domain.Task {
	return &domain.Task{
		ID:       1,
		TaskType: domain.TaskTypeWorkflow,
		Status:   domain.StatusQueued,
		Steps: []domain.TaskStep{
			{ID: 11, StepOrder: 1, Status: domain.StatusPending},
			{ID: 12, StepOrder: 2, Status: domain.StatusPending},
			{ID: 13, StepOrder: 3, Status: domain.StatusPending},
		},
	}
}

func TestTaskExecutorRejectsAgentTask(t *testing.T) {
	task := pendingTask()
	task.TaskType = domain.TaskTypeAgent
	states := &fakeExecutionStateStore{}
	steps := &fakeStepRunner{}
	executor := NewTaskExecutor(&fakeTaskSource{task: task}, states, steps)

	err := executor.Execute(context.Background(), task.ID)
	if !errors.Is(err, ErrTaskNotRunnable) {
		t.Fatalf("Execute() error = %v, want ErrTaskNotRunnable", err)
	}
	if len(states.events) != 0 || len(steps.calls) != 0 {
		t.Fatalf("agent task reached workflow execution: events=%v calls=%v", states.events, steps.calls)
	}
}

func equalIDs(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalEvents(got, want []transitionEvent) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
