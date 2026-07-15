package repository

import (
	"context"
	"encoding/json"
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
