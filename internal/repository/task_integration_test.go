package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

func TestGormTaskRepositoryCreateRollsBackWhenStepsFail(t *testing.T) {
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

	taskName := "rollback-test-" + time.Now().Format("20060102150405.000000000")
	task := &domain.Task{
		Name:     taskName,
		TaskType: domain.TaskTypeWorkflow,
		Status:   domain.StatusPending,
		Steps: []domain.TaskStep{
			{Name: "step one", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{}`), Status: domain.StatusPending},
			{Name: "duplicate order", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{}`), Status: domain.StatusPending},
		},
	}

	repository := NewGormTaskRepository(db)
	if err := repository.Create(context.Background(), task); err == nil {
		t.Fatal("Create() returned nil, want duplicate step order error")
	}

	var count int64
	if err := db.Model(&domain.Task{}).Where("name = ?", taskName).Count(&count).Error; err != nil {
		t.Fatalf("count rollback test tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("task rows after rollback = %d, want 0", count)
	}
}

func TestGormTaskRepositoryListsPagesFiltersAndStepCounts(t *testing.T) {
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
	repo := NewGormTaskRepository(db)
	prefix := "list-page-" + time.Now().Format("150405.000000000")
	tasks := []*domain.Task{
		{Name: prefix + "-workflow", Description: "alpha", TaskType: domain.TaskTypeWorkflow, Status: domain.StatusPending, Steps: []domain.TaskStep{{Name: "one", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`), Status: domain.StatusPending}}},
		{Name: prefix + "-agent", Description: "beta", TaskType: domain.TaskTypeAgent, Status: domain.StatusFailed, Steps: []domain.TaskStep{{Name: "one", StepOrder: 1, ActionType: "rag_query", ActionPayload: json.RawMessage(`{"query":"one"}`), Status: domain.StatusSuccess}, {Name: "two", StepOrder: 2, ActionType: "rag_query", ActionPayload: json.RawMessage(`{"query":"two"}`), Status: domain.StatusFailed}}},
		{Name: prefix + "-queued", Description: "gamma", TaskType: domain.TaskTypeWorkflow, Status: domain.StatusQueued, Steps: []domain.TaskStep{{Name: "one", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`), Status: domain.StatusPending}}},
	}
	for _, task := range tasks {
		if err := repo.Create(context.Background(), task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}
	t.Cleanup(func() {
		ids := []uint64{tasks[0].ID, tasks[1].ID, tasks[2].ID}
		db.Where("task_id IN ?", ids).Delete(&domain.ExecutionLog{})
		db.Where("task_id IN ?", ids).Delete(&domain.TaskStep{})
		db.Where("id IN ?", ids).Delete(&domain.Task{})
	})
	page, err := repo.List(context.Background(), TaskListQuery{Search: prefix, Offset: 0, Limit: 2})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].StepCount != 1 || page.Items[1].StepCount != 2 {
		t.Fatalf("page = %#v", page)
	}
	filtered, err := repo.List(context.Background(), TaskListQuery{Search: prefix, TaskType: domain.TaskTypeAgent, Status: domain.StatusFailed, Limit: 10})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].StepCount != 2 {
		t.Fatalf("filtered=%#v error=%v", filtered, err)
	}
	stats, err := repo.Stats(context.Background())
	if err != nil || stats.Total < 3 || stats.ByStatus[domain.StatusQueued] < 1 {
		t.Fatalf("stats=%#v error=%v", stats, err)
	}
}

func TestGormTaskRepositoryQueueReservation(t *testing.T) {
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
	repo := NewGormTaskRepository(db)
	task := &domain.Task{Name: "reserve-" + time.Now().Format("150405.000000000"), TaskType: domain.TaskTypeWorkflow, Status: domain.StatusPending, Steps: []domain.TaskStep{{Name: "one", StepOrder: 1, ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":1}`), Status: domain.StatusPending}}}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})
	previous, err := repo.ReserveForQueue(context.Background(), task.ID, domain.TaskTypeWorkflow)
	if err != nil || previous != domain.StatusPending {
		t.Fatalf("previous=%s error=%v", previous, err)
	}
	if _, err := repo.ReserveForQueue(context.Background(), task.ID, domain.TaskTypeWorkflow); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("second reservation error = %v", err)
	}
	if err := repo.ReleaseQueueReservation(context.Background(), task.ID, previous); err != nil {
		t.Fatalf("ReleaseQueueReservation() error = %v", err)
	}
	loaded, err := repo.GetByID(context.Background(), task.ID)
	if err != nil || loaded.Status != domain.StatusPending {
		t.Fatalf("status=%s error=%v", loaded.Status, err)
	}
}
