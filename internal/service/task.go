package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/repository"
)

var ErrInvalidInput = errors.New("invalid input")

var supportedActionTypes = map[string]struct{}{
	"sleep":      {},
	"http_mock":  {},
	"shell_mock": {},
}

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

func buildPendingStep(index int, input CreateTaskStepInput) (domain.TaskStep, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d name is required", ErrInvalidInput, index+1)
	}
	if _, ok := supportedActionTypes[input.ActionType]; !ok {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d action type %q is not supported", ErrInvalidInput, index+1, input.ActionType)
	}
	if len(input.ActionPayload) == 0 || !json.Valid(input.ActionPayload) {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d action payload must be valid JSON", ErrInvalidInput, index+1)
	}

	return domain.TaskStep{
		Name:          name,
		StepOrder:     index + 1,
		ActionType:    input.ActionType,
		ActionPayload: append(json.RawMessage(nil), input.ActionPayload...),
		Status:        domain.StatusPending,
	}, nil
}
