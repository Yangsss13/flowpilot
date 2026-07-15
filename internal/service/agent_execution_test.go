package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

func TestAgentExecutionServiceSubmitsAgentTask(t *testing.T) {
	queue := &fakeTaskSubmitter{}
	service := NewAgentExecutionService(
		&fakeTaskRepository{task: &domain.Task{ID: 1, TaskType: domain.TaskTypeAgent, Status: domain.StatusPending}},
		queue,
	)
	if err := service.Submit(context.Background(), 1); err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}
	if len(queue.submitted) != 1 || queue.submitted[0] != 1 {
		t.Fatalf("submitted = %v", queue.submitted)
	}
}

func TestAgentExecutionServiceRejectsWorkflowAndTerminalTask(t *testing.T) {
	tests := []domain.Task{
		{ID: 1, TaskType: domain.TaskTypeWorkflow, Status: domain.StatusPending},
		{ID: 2, TaskType: domain.TaskTypeAgent, Status: domain.StatusSuccess},
	}
	for _, task := range tests {
		queue := &fakeTaskSubmitter{}
		service := NewAgentExecutionService(&fakeTaskRepository{task: &task}, queue)
		if err := service.Submit(context.Background(), task.ID); !errors.Is(err, ErrTaskConflict) {
			t.Fatalf("task=%#v error=%v", task, err)
		}
		if len(queue.submitted) != 0 {
			t.Fatalf("task %d was submitted", task.ID)
		}
	}
}
