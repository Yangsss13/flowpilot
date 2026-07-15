package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

var ErrTaskConflict = errors.New("task cannot be executed in its current state")
var ErrQueueUnavailable = errors.New("task queue is unavailable")

type TaskPublisher interface {
	Publish(ctx context.Context, taskID uint64) error
}

type ExecutionLogSource interface {
	ListLogs(ctx context.Context, taskID uint64) ([]domain.ExecutionLog, error)
}

type ExecutionService struct {
	tasks repository.TaskRepository
	logs  ExecutionLogSource
	queue TaskPublisher
}

func NewExecutionService(tasks repository.TaskRepository, logs ExecutionLogSource, queue TaskPublisher) *ExecutionService {
	return &ExecutionService{tasks: tasks, logs: logs, queue: queue}
}

func (s *ExecutionService) Submit(ctx context.Context, taskID uint64) error {
	task, err := s.tasks.GetByID(ctx, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrTaskNotFound
	}
	if err != nil {
		return fmt.Errorf("load task before submission: %w", err)
	}
	if task.Status != domain.StatusPending && task.Status != domain.StatusFailed {
		return ErrTaskConflict
	}

	if err := s.queue.Publish(ctx, taskID); err != nil {
		return fmt.Errorf("%w: publish task: %v", ErrQueueUnavailable, err)
	}
	return nil
}

func (s *ExecutionService) Logs(ctx context.Context, taskID uint64) ([]domain.ExecutionLog, error) {
	if _, err := s.tasks.GetByID(ctx, taskID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("check task before listing logs: %w", err)
	}

	logs, err := s.logs.ListLogs(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task logs: %w", err)
	}
	return logs, nil
}
