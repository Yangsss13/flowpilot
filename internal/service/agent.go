package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

const maxAgentGoalRunes = 500
const maxTaskNameRunes = 100

var ErrAgentPlanGeneration = errors.New("agent plan generation failed")

type AgentPlanner interface {
	CreatePlan(ctx context.Context, goal string) (agent.Plan, error)
}

type CreateAgentTaskInput struct {
	Name string
	Goal string
}

type AgentService struct {
	planner    AgentPlanner
	repository repository.TaskRepository
}

func NewAgentService(planner AgentPlanner, repository repository.TaskRepository) *AgentService {
	return &AgentService{planner: planner, repository: repository}
}

func (s *AgentService) Create(ctx context.Context, input CreateAgentTaskInput) (*domain.Task, error) {
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return nil, fmt.Errorf("%w: goal is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(goal) > maxAgentGoalRunes {
		return nil, fmt.Errorf("%w: goal exceeds %d characters", ErrInvalidInput, maxAgentGoalRunes)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = truncateRunes(goal, maxTaskNameRunes)
	}
	if utf8.RuneCountInString(name) > maxTaskNameRunes {
		return nil, fmt.Errorf("%w: task name exceeds %d characters", ErrInvalidInput, maxTaskNameRunes)
	}

	plan, err := s.planner.CreatePlan(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAgentPlanGeneration, err)
	}
	task := &domain.Task{
		Name:        name,
		Description: goal,
		TaskType:    domain.TaskTypeAgent,
		Status:      domain.StatusPending,
		Steps:       make([]domain.TaskStep, 0, len(plan.Steps)),
	}
	for index, planStep := range plan.Steps {
		dependencies := planStep.DependsOn
		if dependencies == nil {
			dependencies = []string{}
		}
		dependsOn, err := json.Marshal(dependencies)
		if err != nil {
			return nil, fmt.Errorf("encode dependencies for step %q: %w", planStep.ID, err)
		}
		task.Steps = append(task.Steps, domain.TaskStep{
			Name:          planStep.ID,
			StepOrder:     index + 1,
			ActionType:    string(planStep.Tool),
			ActionPayload: append(json.RawMessage(nil), planStep.Input...),
			DependsOn:     dependsOn,
			Status:        domain.StatusPending,
		})
	}
	if err := s.repository.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create agent task: %w", err)
	}
	return task, nil
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
