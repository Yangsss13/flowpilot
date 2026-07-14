package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Yangsss13/flowpilot/internal/action"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrTaskNotFound = errors.New("task not found")

type CreateTaskInput struct {
	Name        string
	Description string
	Steps       []CreateTaskStepInput
}

type CreateTaskStepInput struct {
	Name          string
	ActionType    string
	ActionPayload json.RawMessage
}

type TaskService struct {
	repository repository.TaskRepository
}

func NewTaskService(repository repository.TaskRepository) *TaskService {
	return &TaskService{repository: repository}
}

func (s *TaskService) Create(ctx context.Context, input CreateTaskInput) (*domain.Task, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: task name is required", ErrInvalidInput)
	}
	if len(input.Steps) == 0 {
		return nil, fmt.Errorf("%w: at least one step is required", ErrInvalidInput)
	}

	task := &domain.Task{
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Status:      domain.StatusPending,
		Steps:       make([]domain.TaskStep, 0, len(input.Steps)),
	}

	for i, inputStep := range input.Steps {
		step, err := buildPendingStep(i, inputStep)
		if err != nil {
			return nil, err
		}
		task.Steps = append(task.Steps, step)
	}

	if err := s.repository.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (s *TaskService) List(ctx context.Context) ([]domain.Task, error) {
	tasks, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

func (s *TaskService) GetByID(ctx context.Context, id uint64) (*domain.Task, error) {
	task, err := s.repository.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

func buildPendingStep(index int, input CreateTaskStepInput) (domain.TaskStep, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d name is required", ErrInvalidInput, index+1)
	}
	if err := action.Validate(input.ActionType, input.ActionPayload); err != nil {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d action is invalid: %v", ErrInvalidInput, index+1, err)
	}

	return domain.TaskStep{
		Name:          name,
		StepOrder:     index + 1,
		ActionType:    input.ActionType,
		ActionPayload: append(json.RawMessage(nil), input.ActionPayload...),
		Status:        domain.StatusPending,
	}, nil
}
