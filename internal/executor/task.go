package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"minikvx-agent/internal/domain"
)

var ErrTaskNotRunnable = errors.New("task is not runnable")
var ErrStepExecution = errors.New("step execution failed")

type TaskSource interface {
	GetByID(ctx context.Context, id uint64) (*domain.Task, error)
}

type ExecutionStateStore interface {
	TransitionTask(ctx context.Context, taskID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	TransitionStep(ctx context.Context, taskID, stepID uint64, current, next domain.Status, level domain.LogLevel, message string) error
}

type StepRunner interface {
	Execute(ctx context.Context, step domain.TaskStep) error
}

type TaskExecutor struct {
	tasks  TaskSource
	states ExecutionStateStore
	steps  StepRunner
}

func NewTaskExecutor(tasks TaskSource, states ExecutionStateStore, steps StepRunner) *TaskExecutor {
	return &TaskExecutor{tasks: tasks, states: states, steps: steps}
}

func (e *TaskExecutor) Execute(ctx context.Context, taskID uint64) error {
	task, err := e.tasks.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load task %d: %w", taskID, err)
	}
	if task.Status != domain.StatusPending && task.Status != domain.StatusFailed {
		return fmt.Errorf("%w: task %d has status %s", ErrTaskNotRunnable, task.ID, task.Status)
	}

	if err := e.states.TransitionTask(
		ctx, task.ID,
		task.Status, domain.StatusRunning,
		domain.LogLevelInfo, "task started",
	); err != nil {
		return fmt.Errorf("start task %d: %w", task.ID, err)
	}

	for i := range task.Steps {
		step := task.Steps[i]
		if step.Status == domain.StatusSuccess {
			continue
		}

		if err := e.states.TransitionStep(
			ctx, task.ID, step.ID,
			step.Status, domain.StatusRunning,
			domain.LogLevelInfo, fmt.Sprintf("step %d started", step.StepOrder),
		); err != nil {
			return e.failTask(ctx, task.ID, fmt.Errorf("start step %d: %w", step.ID, err))
		}

		executionErr := e.steps.Execute(ctx, step)
		finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		if executionErr != nil {
			stepErr := fmt.Errorf("%w: step %d: %v", ErrStepExecution, step.ID, executionErr)
			transitionErr := e.states.TransitionStep(
				finalizeCtx, task.ID, step.ID,
				domain.StatusRunning, domain.StatusFailed,
				domain.LogLevelError, stepErr.Error(),
			)
			cancelFinalize()
			return e.failTask(ctx, task.ID, errors.Join(stepErr, transitionErr))
		}

		if err := e.states.TransitionStep(
			finalizeCtx, task.ID, step.ID,
			domain.StatusRunning, domain.StatusSuccess,
			domain.LogLevelInfo, fmt.Sprintf("step %d succeeded", step.StepOrder),
		); err != nil {
			cancelFinalize()
			return e.failTask(ctx, task.ID, fmt.Errorf("finish step %d: %w", step.ID, err))
		}
		cancelFinalize()
	}

	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancelFinalize()
	if err := e.states.TransitionTask(
		finalizeCtx, task.ID,
		domain.StatusRunning, domain.StatusSuccess,
		domain.LogLevelInfo, "task succeeded",
	); err != nil {
		return fmt.Errorf("finish task %d: %w", task.ID, err)
	}
	return nil
}

func (e *TaskExecutor) failTask(ctx context.Context, taskID uint64, cause error) error {
	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancelFinalize()
	transitionErr := e.states.TransitionTask(
		finalizeCtx, taskID,
		domain.StatusRunning, domain.StatusFailed,
		domain.LogLevelError, cause.Error(),
	)
	return errors.Join(cause, transitionErr)
}
