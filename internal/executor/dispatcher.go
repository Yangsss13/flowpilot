package executor

import (
	"context"
	"fmt"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

type TaskRunner interface {
	Execute(ctx context.Context, taskID uint64) error
}

type TaskDispatcher struct {
	tasks    TaskSource
	workflow TaskRunner
	agent    TaskRunner
}

func NewTaskDispatcher(tasks TaskSource, workflow, agent TaskRunner) *TaskDispatcher {
	return &TaskDispatcher{tasks: tasks, workflow: workflow, agent: agent}
}

func (d *TaskDispatcher) Execute(ctx context.Context, taskID uint64) error {
	task, err := d.tasks.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load task for dispatch: %w", err)
	}
	switch task.TaskType {
	case domain.TaskTypeWorkflow:
		if d.workflow == nil {
			return fmt.Errorf("%w: workflow runner is unavailable", ErrTaskNotRunnable)
		}
		return d.workflow.Execute(ctx, taskID)
	case domain.TaskTypeAgent:
		if d.agent == nil {
			return fmt.Errorf("%w: agent runner is unavailable", ErrTaskNotRunnable)
		}
		return d.agent.Execute(ctx, taskID)
	default:
		return fmt.Errorf("%w: unsupported task type %q", ErrTaskNotRunnable, task.TaskType)
	}
}
