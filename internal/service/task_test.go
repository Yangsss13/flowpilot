package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

type fakeTaskRepository struct {
	created   *domain.Task
	page      repository.TaskListPage
	listQuery repository.TaskListQuery
	stats     repository.TaskStats
	task      *domain.Task
	err       error
	calls     int
	released  bool
	deleted   bool
}

func (f *fakeTaskRepository) Create(_ context.Context, task *domain.Task) error {
	f.calls++
	f.created = task
	return f.err
}

func (f *fakeTaskRepository) List(_ context.Context, query repository.TaskListQuery) (repository.TaskListPage, error) {
	f.calls++
	f.listQuery = query
	return f.page, f.err
}

func (f *fakeTaskRepository) Stats(_ context.Context) (repository.TaskStats, error) {
	f.calls++
	return f.stats, f.err
}

func (f *fakeTaskRepository) ReserveForQueue(_ context.Context, _ uint64, taskType domain.TaskType) (domain.Status, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if f.task == nil {
		return "", repository.ErrNotFound
	}
	if f.task.TaskType != taskType || (f.task.Status != domain.StatusPending && f.task.Status != domain.StatusFailed) {
		return "", repository.ErrStateConflict
	}
	previous := f.task.Status
	f.task.Status = domain.StatusQueued
	return previous, nil
}

func (f *fakeTaskRepository) ReleaseQueueReservation(_ context.Context, _ uint64, previous domain.Status) error {
	f.calls++
	f.released = true
	if f.task != nil && f.task.Status == domain.StatusQueued {
		f.task.Status = previous
	}
	return nil
}

func (f *fakeTaskRepository) GetByID(_ context.Context, _ uint64) (*domain.Task, error) {
	f.calls++
	return f.task, f.err
}

func (f *fakeTaskRepository) DeleteInactive(_ context.Context, _ uint64) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	if f.task == nil {
		return repository.ErrNotFound
	}
	if f.task.Status == domain.StatusQueued || f.task.Status == domain.StatusRunning {
		return repository.ErrStateConflict
	}
	f.deleted = true
	return nil
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
	if task.TaskType != domain.TaskTypeWorkflow {
		t.Fatalf("task type = %q, want %q", task.TaskType, domain.TaskTypeWorkflow)
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

func TestTaskServiceCreateValidatesFieldBoundaries(t *testing.T) {
	manySteps := make([]CreateTaskStepInput, MaxWorkflowSteps+1)
	for index := range manySteps {
		manySteps[index] = validSteps()[0]
	}
	tests := []struct {
		name    string
		input   CreateTaskInput
		wantErr bool
	}{
		{name: "task name 100", input: CreateTaskInput{Name: strings.Repeat("名", 100), Steps: validSteps()}},
		{name: "task name 101", input: CreateTaskInput{Name: strings.Repeat("名", 101), Steps: validSteps()}, wantErr: true},
		{name: "description 500", input: CreateTaskInput{Name: "task", Description: strings.Repeat("描", 500), Steps: validSteps()}},
		{name: "description 501", input: CreateTaskInput{Name: "task", Description: strings.Repeat("描", 501), Steps: validSteps()}, wantErr: true},
		{name: "step name 100", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: strings.Repeat("步", 100), ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`)}}}},
		{name: "step name 101", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: strings.Repeat("步", 101), ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`)}}}, wantErr: true},
		{name: "too many steps", input: CreateTaskInput{Name: "task", Steps: manySteps}, wantErr: true},
		{name: "oversized action payload", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: "step", ActionType: "sleep", ActionPayload: json.RawMessage(strings.Repeat(" ", MaxActionPayloadBytes+1))}}}, wantErr: true},
		{name: "unknown action field", input: CreateTaskInput{Name: "task", Steps: []CreateTaskStepInput{{Name: "step", ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1,"extra":true}`)}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTaskRepository{}
			_, err := NewTaskService(repo).Create(context.Background(), tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) || repo.calls != 0 {
					t.Fatalf("error=%v repository calls=%d", err, repo.calls)
				}
				return
			}
			if err != nil || repo.calls != 1 {
				t.Fatalf("error=%v repository calls=%d", err, repo.calls)
			}
		})
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

func TestTaskServiceDeleteMapsRepositoryResults(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeTaskRepository
		want error
	}{
		{name: "success", repo: &fakeTaskRepository{task: &domain.Task{ID: 1, Status: domain.StatusSuccess}}},
		{name: "not found", repo: &fakeTaskRepository{}, want: ErrTaskNotFound},
		{name: "active", repo: &fakeTaskRepository{task: &domain.Task{ID: 1, Status: domain.StatusRunning}}, want: ErrTaskConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewTaskService(tt.repo).Delete(context.Background(), 1)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Delete() error = %v, want %v", err, tt.want)
			}
			if tt.want == nil && !tt.repo.deleted {
				t.Fatal("task was not deleted")
			}
		})
	}
}

func TestTaskServiceListPaginatesFiltersAndMapsStepCount(t *testing.T) {
	repo := &fakeTaskRepository{page: repository.TaskListPage{
		Items: []repository.TaskListItem{{ID: 7, Name: "agent", TaskType: domain.TaskTypeAgent, Status: domain.StatusQueued, StepCount: 3}},
		Total: 1,
	}}
	result, err := NewTaskService(repo).List(context.Background(), ListTasksInput{
		Page: 2, PageSize: 10, TaskType: "agent", Status: "Queued", Query: "agent",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || result.Page != 2 || result.PageSize != 10 || len(result.Items) != 1 || result.Items[0].StepCount != 3 {
		t.Fatalf("result = %#v", result)
	}
	if repo.listQuery.Offset != 10 || repo.listQuery.Limit != 10 || repo.listQuery.TaskType != domain.TaskTypeAgent || repo.listQuery.Status != domain.StatusQueued {
		t.Fatalf("repository query = %#v", repo.listQuery)
	}
}

func TestTaskServiceListRejectsInvalidFilters(t *testing.T) {
	tests := []ListTasksInput{
		{Page: -1},
		{Page: 1, PageSize: MaxTaskPageSize + 1},
		{TaskType: "unknown"},
		{Status: "unknown"},
		{Query: strings.Repeat("q", 201)},
	}
	for _, input := range tests {
		repo := &fakeTaskRepository{}
		if _, err := NewTaskService(repo).List(context.Background(), input); !errors.Is(err, ErrInvalidInput) || repo.calls != 0 {
			t.Fatalf("input=%#v error=%v calls=%d", input, err, repo.calls)
		}
	}
}
