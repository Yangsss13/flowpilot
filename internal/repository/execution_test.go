package repository

import (
	"context"
	"testing"

	"minikvx-agent/internal/domain"
)

func TestTransitionTaskRejectsIllegalTransitionBeforeDatabase(t *testing.T) {
	repository := NewGormExecutionRepository(nil)
	err := repository.TransitionTask(
		context.Background(),
		1,
		domain.StatusSuccess,
		domain.StatusRunning,
		domain.LogLevelInfo,
		"should not run",
	)
	if err == nil {
		t.Fatal("TransitionTask() returned nil, want illegal transition error")
	}
}
