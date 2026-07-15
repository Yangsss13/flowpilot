package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

type AgentExecutionService struct {
	tasks repository.TaskRepository
	queue TaskPublisher
}

func NewAgentExecutionService(tasks repository.TaskRepository, queue TaskPublisher) *AgentExecutionService {
	return &AgentExecutionService{tasks: tasks, queue: queue}
}

func (s *AgentExecutionService) Submit(ctx context.Context, taskID uint64) error {
	task, err := s.tasks.GetByID(ctx, taskID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrTaskNotFound
	}
	if err != nil {
		return fmt.Errorf("load agent task before submission: %w", err)
	}
	if task.TaskType != domain.TaskTypeAgent || (task.Status != domain.StatusPending && task.Status != domain.StatusFailed) {
		return ErrTaskConflict
	}
	if err := s.queue.Publish(ctx, taskID); err != nil {
		return fmt.Errorf("%w: publish agent task: %v", ErrQueueUnavailable, err)
	}
	return nil
}
