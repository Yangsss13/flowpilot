package service

import (
	"context"

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
	return reserveAndPublish(ctx, s.tasks, s.queue, taskID, domain.TaskTypeAgent)
}
