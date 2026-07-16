package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yangsss13/flowpilot/internal/action"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrTaskNotFound = errors.New("task not found")

const (
	MaxTaskNameRunes        = 100
	MaxTaskDescriptionRunes = 500
	MaxStepNameRunes        = 100
	MaxWorkflowSteps        = 100
	MaxActionPayloadBytes   = 64 << 10
	MaxTaskRequestBytes     = 1 << 20
	DefaultTaskPageSize     = 20
	MaxTaskPageSize         = 100
)

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

type ListTasksInput struct {
	Page     int
	PageSize int
	TaskType string
	Status   string
	Query    string
}

type TaskListItem struct {
	ID          uint64          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	TaskType    domain.TaskType `json:"task_type"`
	Status      domain.Status   `json:"status"`
	StepCount   int64           `json:"step_count"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type TaskListResult struct {
	Items    []TaskListItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type TaskStatsResult struct {
	Total    int64                     `json:"total"`
	ByStatus map[domain.Status]int64   `json:"by_status"`
	ByType   map[domain.TaskType]int64 `json:"by_type"`
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
	if utf8.RuneCountInString(name) > MaxTaskNameRunes {
		return nil, fmt.Errorf("%w: task name must not exceed %d characters", ErrInvalidInput, MaxTaskNameRunes)
	}
	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > MaxTaskDescriptionRunes {
		return nil, fmt.Errorf("%w: task description must not exceed %d characters", ErrInvalidInput, MaxTaskDescriptionRunes)
	}
	if len(input.Steps) == 0 {
		return nil, fmt.Errorf("%w: at least one step is required", ErrInvalidInput)
	}
	if len(input.Steps) > MaxWorkflowSteps {
		return nil, fmt.Errorf("%w: step count must not exceed %d", ErrInvalidInput, MaxWorkflowSteps)
	}

	task := &domain.Task{
		Name:        name,
		Description: description,
		TaskType:    domain.TaskTypeWorkflow,
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

func (s *TaskService) List(ctx context.Context, input ListTasksInput) (TaskListResult, error) {
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = DefaultTaskPageSize
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > MaxTaskPageSize {
		return TaskListResult{}, fmt.Errorf("%w: page must be positive and page_size must be between 1 and %d", ErrInvalidInput, MaxTaskPageSize)
	}
	var taskType domain.TaskType
	if input.TaskType != "" {
		taskType = domain.TaskType(strings.ToLower(strings.TrimSpace(input.TaskType)))
		if taskType != domain.TaskTypeWorkflow && taskType != domain.TaskTypeAgent {
			return TaskListResult{}, fmt.Errorf("%w: unsupported task_type", ErrInvalidInput)
		}
	}
	var status domain.Status
	if input.Status != "" {
		status = domain.Status(strings.TrimSpace(input.Status))
		if !status.IsValid() {
			return TaskListResult{}, fmt.Errorf("%w: unsupported status", ErrInvalidInput)
		}
	}
	query := strings.TrimSpace(input.Query)
	if utf8.RuneCountInString(query) > 200 {
		return TaskListResult{}, fmt.Errorf("%w: query must not exceed 200 characters", ErrInvalidInput)
	}
	page, err := s.repository.List(ctx, repository.TaskListQuery{
		Offset: (input.Page - 1) * input.PageSize, Limit: input.PageSize,
		TaskType: taskType, Status: status, Search: query,
	})
	if err != nil {
		return TaskListResult{}, fmt.Errorf("list tasks: %w", err)
	}
	items := make([]TaskListItem, len(page.Items))
	for index, item := range page.Items {
		items[index] = TaskListItem{
			ID: item.ID, Name: item.Name, Description: item.Description,
			TaskType: item.TaskType, Status: item.Status, StepCount: item.StepCount,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		}
	}
	return TaskListResult{Items: items, Total: page.Total, Page: input.Page, PageSize: input.PageSize}, nil
}

func (s *TaskService) Stats(ctx context.Context) (TaskStatsResult, error) {
	stats, err := s.repository.Stats(ctx)
	if err != nil {
		return TaskStatsResult{}, fmt.Errorf("get task stats: %w", err)
	}
	return TaskStatsResult{Total: stats.Total, ByStatus: stats.ByStatus, ByType: stats.ByType}, nil
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

func (s *TaskService) Delete(ctx context.Context, id uint64) error {
	err := s.repository.DeleteInactive(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrTaskNotFound
	}
	if errors.Is(err, repository.ErrStateConflict) {
		return ErrTaskConflict
	}
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

func buildPendingStep(index int, input CreateTaskStepInput) (domain.TaskStep, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d name is required", ErrInvalidInput, index+1)
	}
	if utf8.RuneCountInString(name) > MaxStepNameRunes {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d name must not exceed %d characters", ErrInvalidInput, index+1, MaxStepNameRunes)
	}
	if len(input.ActionPayload) == 0 || len(input.ActionPayload) > MaxActionPayloadBytes {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d action payload must be between 1 and %d bytes", ErrInvalidInput, index+1, MaxActionPayloadBytes)
	}
	actionType := strings.TrimSpace(input.ActionType)
	if err := action.Validate(actionType, input.ActionPayload); err != nil {
		return domain.TaskStep{}, fmt.Errorf("%w: step %d action is invalid: %v", ErrInvalidInput, index+1, err)
	}

	return domain.TaskStep{
		Name:          name,
		StepOrder:     index + 1,
		ActionType:    actionType,
		ActionPayload: append(json.RawMessage(nil), input.ActionPayload...),
		Status:        domain.StatusPending,
	}, nil
}
