package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

func TestAgentRuntimePersistenceWithMySQL(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	task := &domain.Task{
		Name: "agent-runtime-" + time.Now().Format("20060102150405.000000000"), Description: "goal",
		TaskType: domain.TaskTypeAgent, Status: domain.StatusPending,
		Steps: []domain.TaskStep{{Name: "old", StepOrder: 1, ActionType: string(agent.ToolRAGQuery), ActionPayload: json.RawMessage(`{"query":"old"}`), Status: domain.StatusPending}},
	}
	tasks := NewGormTaskRepository(db)
	states := NewGormExecutionRepository(db)
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})
	if err := states.TransitionTask(context.Background(), task.ID, domain.StatusPending, domain.StatusRunning, domain.LogLevelInfo, "started"); err != nil {
		t.Fatalf("start task: %v", err)
	}
	plan := agent.Plan{Steps: []agent.PlanStep{{ID: "search", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"refund"}`)}}}
	steps, err := states.ReplaceAgentPlan(context.Background(), task.ID, plan, 1)
	if err != nil {
		t.Fatalf("replace plan: %v", err)
	}
	if err := states.TransitionStep(context.Background(), task.ID, steps[0].ID, domain.StatusPending, domain.StatusRunning, domain.LogLevelInfo, "step started"); err != nil {
		t.Fatalf("start step: %v", err)
	}
	observation := agent.Observation{StepID: "search", Output: json.RawMessage(`{"results":["policy"]}`)}
	if err := states.CompleteAgentStep(context.Background(), task.ID, steps[0].ID, domain.StatusSuccess, observation); err != nil {
		t.Fatalf("complete step: %v", err)
	}
	if err := states.CompleteAgentTask(context.Background(), task.ID, domain.StatusSuccess, "seven days", "finished"); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	loaded, err := tasks.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if loaded.Status != domain.StatusSuccess || loaded.Result != "seven days" || loaded.ReplanCount != 1 || len(loaded.Steps) != 1 || len(loaded.Steps[0].Observation) == 0 {
		t.Fatalf("persisted agent task = %#v", loaded)
	}
}

func TestCompleteAgentTaskRequiresEveryStepSuccessful(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	tests := []struct {
		name       string
		lastStatus domain.Status
		wantErr    bool
	}{
		{name: "pending", lastStatus: domain.StatusPending, wantErr: true},
		{name: "running", lastStatus: domain.StatusRunning, wantErr: true},
		{name: "failed", lastStatus: domain.StatusFailed, wantErr: true},
		{name: "success", lastStatus: domain.StatusSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &domain.Task{
				Name:        "finish-invariant-" + tt.name + "-" + time.Now().Format("150405.000000000"),
				Description: "goal", TaskType: domain.TaskTypeAgent, Status: domain.StatusRunning,
				Steps: []domain.TaskStep{
					{Name: "done", StepOrder: 1, ActionType: string(agent.ToolRAGQuery), ActionPayload: json.RawMessage(`{"query":"done"}`), Status: domain.StatusSuccess},
					{Name: "last", StepOrder: 2, ActionType: string(agent.ToolRAGQuery), ActionPayload: json.RawMessage(`{"query":"last"}`), Status: tt.lastStatus},
				},
			}
			tasks := NewGormTaskRepository(db)
			states := NewGormExecutionRepository(db)
			if err := tasks.Create(context.Background(), task); err != nil {
				t.Fatalf("create task: %v", err)
			}
			t.Cleanup(func() {
				db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
				db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
				db.Delete(&domain.Task{}, task.ID)
			})
			err := states.CompleteAgentTask(context.Background(), task.ID, domain.StatusSuccess, "answer", "done")
			if tt.wantErr {
				if !errors.Is(err, ErrIncompleteAgentSteps) {
					t.Fatalf("CompleteAgentTask() error = %v", err)
				}
			} else if err != nil {
				t.Fatalf("CompleteAgentTask() error = %v", err)
			}
			loaded, err := tasks.GetByID(context.Background(), task.ID)
			if err != nil {
				t.Fatalf("reload task: %v", err)
			}
			wantStatus := domain.StatusSuccess
			if tt.wantErr {
				wantStatus = domain.StatusRunning
			}
			if loaded.Status != wantStatus {
				t.Fatalf("task status = %s, want %s", loaded.Status, wantStatus)
			}
		})
	}
}

func TestInterruptAgentTaskWithMySQL(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	task := &domain.Task{
		Name:        "agent-interruption-" + time.Now().Format("20060102150405.000000000"),
		Description: "goal", TaskType: domain.TaskTypeAgent, Status: domain.StatusRunning,
		Steps: []domain.TaskStep{{
			Name: "external-call", StepOrder: 1, ActionType: string(agent.ToolHTTPRequest),
			ActionPayload: json.RawMessage(`{"method":"POST","url":"https://example.com"}`), Status: domain.StatusRunning,
		}},
	}
	tasks := NewGormTaskRepository(db)
	states := NewGormExecutionRepository(db)
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})
	reason := "agent stopped while a tool may have been executing"
	observation := agent.Observation{StepID: task.Steps[0].Name, Error: reason}
	if err := states.InterruptAgentTask(context.Background(), task.ID, task.Steps[0].ID, observation, reason); err != nil {
		t.Fatalf("InterruptAgentTask() error = %v", err)
	}
	loaded, err := tasks.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if loaded.Status != domain.StatusFailed || loaded.Steps[0].Status != domain.StatusFailed || len(loaded.Steps[0].Observation) == 0 {
		t.Fatalf("interrupted task = %#v", loaded)
	}
}
