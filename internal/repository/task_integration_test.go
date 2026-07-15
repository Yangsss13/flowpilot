package repository

import (
	"context"
	"encoding/json"
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
		Name:   taskName,
		Status: domain.StatusPending,
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
