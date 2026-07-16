package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

var ErrTaskNotRunnable = errors.New("task is not runnable")
var ErrStepExecution = errors.New("step execution failed")

type TaskSource interface {
	GetByID(ctx context.Context, id uint64) (*domain.Task, error)
}

type ExecutionStateStore interface {
	TransitionTask(ctx context.Context, taskID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	TransitionStep(ctx context.Context, taskID, stepID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	CompleteWorkflowStep(ctx context.Context, taskID, stepID uint64, observation json.RawMessage, taskResult string, level domain.LogLevel, message string) error
}

type StepRunner interface {
	Execute(ctx context.Context, step domain.TaskStep) (json.RawMessage, error)
}

type ContextualStepRunner interface {
	ExecuteWithPrevious(ctx context.Context, step domain.TaskStep, previous []domain.TaskStep) (json.RawMessage, error)
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
	if task.TaskType != domain.TaskTypeWorkflow {
		return fmt.Errorf("%w: task %d has type %s", ErrTaskNotRunnable, task.ID, task.TaskType)
	}
	if task.Status != domain.StatusQueued {
		return fmt.Errorf("%w: task %d has status %s", ErrTaskNotRunnable, task.ID, task.Status)
	}

	if err := e.states.TransitionTask(
		ctx, task.ID,
		domain.StatusQueued, domain.StatusRunning,
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

		var observation json.RawMessage
		var executionErr error
		if contextual, ok := e.steps.(ContextualStepRunner); ok {
			observation, executionErr = contextual.ExecuteWithPrevious(ctx, step, task.Steps[:i])
		} else {
			observation, executionErr = e.steps.Execute(ctx, step)
		}
		taskResult := ""
		if executionErr == nil {
			taskResult, executionErr = summaryTaskResult(step.ActionType, observation)
		}
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

		if err := e.states.CompleteWorkflowStep(
			finalizeCtx, task.ID, step.ID, observation, taskResult,
			domain.LogLevelInfo, fmt.Sprintf("step %d succeeded", step.StepOrder),
		); err != nil {
			cancelFinalize()
			return e.failTask(ctx, task.ID, fmt.Errorf("finish step %d: %w", step.ID, err))
		}
		task.Steps[i].Status = domain.StatusSuccess
		task.Steps[i].Observation = append(json.RawMessage(nil), observation...)
		if taskResult != "" {
			task.Result = taskResult
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

func summaryTaskResult(actionType string, observation json.RawMessage) (string, error) {
	if actionType != "llm_summarize" {
		return "", nil
	}
	var output struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(observation, &output); err != nil {
		return "", fmt.Errorf("decode llm_summarize observation: %w", err)
	}
	if output.Summary == "" {
		return "", fmt.Errorf("llm_summarize observation has no summary")
	}
	return output.Summary, nil
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
