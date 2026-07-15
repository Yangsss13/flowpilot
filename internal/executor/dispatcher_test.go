package executor

import (
	"context"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

type recordingRunner struct {
	calls []uint64
}

func (r *recordingRunner) Execute(_ context.Context, taskID uint64) error {
	r.calls = append(r.calls, taskID)
	return nil
}

func TestTaskDispatcherRoutesByTaskType(t *testing.T) {
	workflow := &recordingRunner{}
	agentRunner := &recordingRunner{}
	source := &fakeTaskSource{task: &domain.Task{ID: 1, TaskType: domain.TaskTypeAgent}}
	dispatcher := NewTaskDispatcher(source, workflow, agentRunner)
	if err := dispatcher.Execute(context.Background(), 1); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if len(agentRunner.calls) != 1 || len(workflow.calls) != 0 {
		t.Fatalf("workflow calls=%v agent calls=%v", workflow.calls, agentRunner.calls)
	}
	source.task.TaskType = domain.TaskTypeWorkflow
	if err := dispatcher.Execute(context.Background(), 1); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if len(workflow.calls) != 1 {
		t.Fatalf("workflow calls=%v", workflow.calls)
	}
}
