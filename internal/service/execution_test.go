package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

type fakeTaskSubmitter struct {
	submitted []uint64
	err       error
}

func (f *fakeTaskSubmitter) Publish(_ context.Context, taskID uint64) error {
	f.submitted = append(f.submitted, taskID)
	return f.err
}

type fakeExecutionLogSource struct {
	logs []domain.ExecutionLog
	err  error
}

func (f *fakeExecutionLogSource) ListLogs(_ context.Context, _ uint64) ([]domain.ExecutionLog, error) {
	return f.logs, f.err
}

func TestExecutionServiceSubmit(t *testing.T) {
	tasks := &fakeTaskRepository{task: &domain.Task{ID: 1, Status: domain.StatusPending}}
	queue := &fakeTaskSubmitter{}
	service := NewExecutionService(tasks, &fakeExecutionLogSource{}, queue)

	if err := service.Submit(context.Background(), 1); err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}
	if len(queue.submitted) != 1 || queue.submitted[0] != 1 {
		t.Fatalf("submitted IDs = %v, want [1]", queue.submitted)
	}
}

func TestExecutionServiceSubmitMapsConflict(t *testing.T) {
	service := NewExecutionService(
		&fakeTaskRepository{task: &domain.Task{ID: 1, Status: domain.StatusSuccess}},
		&fakeExecutionLogSource{},
		&fakeTaskSubmitter{},
	)

	err := service.Submit(context.Background(), 1)
	if !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("Submit() error = %v, want ErrTaskConflict", err)
	}
}

func TestExecutionServiceSubmitMapsUnavailableQueue(t *testing.T) {
	service := NewExecutionService(
		&fakeTaskRepository{task: &domain.Task{ID: 1, Status: domain.StatusPending}},
		&fakeExecutionLogSource{},
		&fakeTaskSubmitter{err: errors.New("RabbitMQ unavailable")},
	)

	err := service.Submit(context.Background(), 1)
	if !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("Submit() error = %v, want ErrQueueUnavailable", err)
	}
}

func TestExecutionServiceLogsChecksTaskExists(t *testing.T) {
	tasks := &fakeTaskRepository{task: &domain.Task{ID: 1}}
	logSource := &fakeExecutionLogSource{logs: []domain.ExecutionLog{{ID: 1, TaskID: 1}}}
	service := NewExecutionService(tasks, logSource, &fakeTaskSubmitter{})

	logs, err := service.Logs(context.Background(), 1)
	if err != nil {
		t.Fatalf("Logs() returned error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs length = %d, want 1", len(logs))
	}
}
