package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

func TestExecutionTransitionsWithMySQL(t *testing.T) {
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
		Name:   "transition-test-" + time.Now().Format("20060102150405.000000000"),
		Status: domain.StatusPending,
		Steps: []domain.TaskStep{{
			Name:          "step",
			StepOrder:     1,
			ActionType:    "sleep",
			ActionPayload: json.RawMessage(`{"duration_ms":1}`),
			Status:        domain.StatusPending,
		}},
	}
	if err := NewGormTaskRepository(db).Create(context.Background(), task); err != nil {
		t.Fatalf("create fixture task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})

	executionRepository := NewGormExecutionRepository(db)
	if err := executionRepository.TransitionTask(
		context.Background(), task.ID,
		domain.StatusPending, domain.StatusRunning,
		domain.LogLevelInfo, "task started",
	); err != nil {
		t.Fatalf("transition task: %v", err)
	}

	if err := executionRepository.TransitionTask(
		context.Background(), task.ID,
		domain.StatusPending, domain.StatusRunning,
		domain.LogLevelInfo, "duplicate task start",
	); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("duplicate transition error = %v, want ErrStateConflict", err)
	}

	stepID := task.Steps[0].ID
	if err := executionRepository.TransitionStep(
		context.Background(), task.ID, stepID,
		domain.StatusPending, domain.StatusRunning,
		domain.LogLevelInfo, "step started",
	); err != nil {
		t.Fatalf("transition step: %v", err)
	}
	observation := json.RawMessage(`{"query":"架构","results":[{"source":"README.md","text":"FlowPilot","score":0.9}]}`)
	if err := executionRepository.CompleteWorkflowStep(
		context.Background(), task.ID, stepID, observation,
		domain.LogLevelInfo, "step succeeded",
	); err != nil {
		t.Fatalf("complete workflow step: %v", err)
	}
	loaded, err := NewGormTaskRepository(db).GetByID(context.Background(), task.ID)
	if err != nil || len(loaded.Steps) != 1 || loaded.Steps[0].Status != domain.StatusSuccess {
		t.Fatalf("completed step=%#v error=%v", loaded, err)
	}
	var gotObservation, wantObservation any
	if err := json.Unmarshal(loaded.Steps[0].Observation, &gotObservation); err != nil {
		t.Fatalf("decode stored observation: %v", err)
	}
	if err := json.Unmarshal(observation, &wantObservation); err != nil {
		t.Fatalf("decode expected observation: %v", err)
	}
	if !reflect.DeepEqual(gotObservation, wantObservation) {
		t.Fatalf("stored observation=%s want=%s", loaded.Steps[0].Observation, observation)
	}

	var logs []domain.ExecutionLog
	if err := db.Where("task_id = ?", task.ID).Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("query transition logs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("log count = %d, want 3", len(logs))
	}
	if logs[0].StepID != nil {
		t.Fatalf("task log step_id = %v, want nil", *logs[0].StepID)
	}
	if logs[1].StepID == nil || *logs[1].StepID != stepID {
		t.Fatalf("step log step_id = %v, want %d", logs[1].StepID, stepID)
	}
}
