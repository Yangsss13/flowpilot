package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/repository"
)

type fakeTaskRepository struct {
	created *domain.Task
	tasks   []domain.Task
	task    *domain.Task
	err     error
	calls   int
}

func (f *fakeTaskRepository) Create(_ context.Context, task *domain.Task) error {
	f.calls++
	f.created = task
	return f.err
}

func (f *fakeTaskRepository) List(_ context.Context) ([]domain.Task, error) {
	f.calls++
	return f.tasks, f.err
}

func (f *fakeTaskRepository) GetByID(_ context.Context, _ uint64) (*domain.Task, error) {
	f.calls++
	return f.task, f.err
}

func TestTaskServiceCreateBuildsPendingOrderedTask(t *testing.T) {
	repo := &fakeTaskRepository{}
	service := NewTaskService(repo)

	task, err := service.Create(context.Background(), CreateTaskInput{
		Name:        "  Generate report  ",
		Description: "  daily workflow  ",
		Steps: []CreateTaskStepInput{
			{Name: "  Fetch data  ", ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":100}`)},
			{Name: "Build report", ActionType: "http_mock", ActionPayload: json.RawMessage(`{"status":200}`)},
		},
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repo.calls)
	}
	if task != repo.created {
		t.Fatal("service did not return the task passed to repository")
	}
	if task.Name != "Generate report" || task.Description != "daily workflow" {
		t.Fatalf("task text was not trimmed: %#v", task)
	}
	if task.Status != domain.StatusPending {
		t.Fatalf("task status = %q, want %q", task.Status, domain.StatusPending)
	}
	if len(task.Steps) != 2 {
		t.Fatalf("steps length = %d, want 2", len(task.Steps))
	}
	for i, step := range task.Steps {
		if step.StepOrder != i+1 {
			t.Errorf("step %d order = %d, want %d", i, step.StepOrder, i+1)
		}
		if step.Status != domain.StatusPending {
			t.Errorf("step %d status = %q, want %q", i, step.Status, domain.StatusPending)
		}
	}
}

func TestTaskServiceCreateRejectsInvalidInputBeforeRepository(t *testing.T) {
	tests := []struct {
		name  string
		input CreateTaskInput
	}{
		{name: "empty task name", input: CreateTaskInput{Name: " ", Steps: validSteps()}},
		{name: "no steps", input: CreateTaskInput{Name: "task"}},
		{name: "empty step name", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: " ", ActionType: "sleep", ActionPayload: json.RawMessage(`{}`)}}}},
		{name: "unsupported action", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: "step", ActionType: "real_shell", ActionPayload: json.RawMessage(`{}`)}}}},
		{name: "invalid payload", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: "step", ActionType: "sleep", ActionPayload: json.RawMessage(`{`)}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTaskRepository{}
			service := NewTaskService(repo)

			_, err := service.Create(context.Background(), tt.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Create() error = %v, want ErrInvalidInput", err)
			}
			if repo.calls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.calls)
			}
		})
	}
}

func TestTaskServiceCreateWrapsRepositoryError(t *testing.T) {
	repositoryErr := errors.New("database unavailable")
	repo := &fakeTaskRepository{err: repositoryErr}
	service := NewTaskService(repo)

	_, err := service.Create(context.Background(), CreateTaskInput{Name: "task", Steps: validSteps()})
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("Create() error = %v, want wrapped repository error", err)
	}
}

func validSteps() []CreateTaskStepInput {
	return []CreateTaskStepInput{{
		Name:          "step",
		ActionType:    "sleep",
		ActionPayload: json.RawMessage(`{"duration_ms":100}`),
	}}
}

func TestTaskServiceGetByIDMapsNotFound(t *testing.T) {
	repo := &fakeTaskRepository{err: repository.ErrNotFound}
	service := NewTaskService(repo)

	_, err := service.GetByID(context.Background(), 999)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("GetByID() error = %v, want ErrTaskNotFound", err)
	}
}
