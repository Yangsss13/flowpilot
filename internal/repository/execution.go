package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

var ErrStateConflict = errors.New("state conflict")

type ExecutionRepository interface {
	TransitionTask(ctx context.Context, taskID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	TransitionStep(ctx context.Context, taskID, stepID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	CompleteWorkflowStep(ctx context.Context, taskID, stepID uint64, observation json.RawMessage, taskResult string, level domain.LogLevel, message string) error
}

// CompleteWorkflowStep persists the output and Running -> Success transition
// in one transaction. A successful workflow step must never lose the output
// that explains what the real action produced.
func (r *GormExecutionRepository) CompleteWorkflowStep(
	ctx context.Context,
	taskID, stepID uint64,
	observation json.RawMessage,
	taskResult string,
	level domain.LogLevel,
	message string,
) error {
	if err := domain.ValidateTransition(domain.StatusRunning, domain.StatusSuccess); err != nil {
		return fmt.Errorf("validate step transition: %w", err)
	}
	if len(observation) == 0 || !json.Valid(observation) {
		return fmt.Errorf("workflow step observation must be valid JSON")
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.TaskStep{}).
			Where("id = ? AND task_id = ? AND status = ?", stepID, taskID, domain.StatusRunning).
			Updates(map[string]any{"status": domain.StatusSuccess, "observation": observation})
		if result.Error != nil {
			return fmt.Errorf("complete workflow step: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		if taskResult != "" {
			result = tx.Model(&domain.Task{}).
				Where("id = ? AND status = ?", taskID, domain.StatusRunning).
				Update("result", taskResult)
			if result.Error != nil {
				return fmt.Errorf("save workflow result: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrStateConflict
			}
		}

		logEntry := domain.ExecutionLog{TaskID: taskID, StepID: &stepID, Level: level, Message: message}
		if err := tx.Create(&logEntry).Error; err != nil {
			return fmt.Errorf("create step transition log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete workflow step: %w", err)
	}
	return nil
}

type GormExecutionRepository struct {
	db *gorm.DB
}

func NewGormExecutionRepository(db *gorm.DB) *GormExecutionRepository {
	return &GormExecutionRepository{db: db}
}

func (r *GormExecutionRepository) ListLogs(ctx context.Context, taskID uint64) ([]domain.ExecutionLog, error) {
	var logs []domain.ExecutionLog
	if err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at ASC, id ASC").
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("list execution logs: %w", err)
	}
	return logs, nil
}

func (r *GormExecutionRepository) TransitionTask(
	ctx context.Context,
	taskID uint64,
	current, next domain.Status,
	level domain.LogLevel,
	message string,
) error {
	if err := domain.ValidateTransition(current, next); err != nil {
		return fmt.Errorf("validate task transition: %w", err)
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND status = ?", taskID, current).
			Update("status", next)
		if result.Error != nil {
			return fmt.Errorf("update task status: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}

		logEntry := domain.ExecutionLog{
			TaskID:  taskID,
			Level:   level,
			Message: message,
		}
		if err := tx.Create(&logEntry).Error; err != nil {
			return fmt.Errorf("create task transition log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("transition task: %w", err)
	}
	return nil
}

func (r *GormExecutionRepository) TransitionStep(
	ctx context.Context,
	taskID, stepID uint64,
	current, next domain.Status,
	level domain.LogLevel,
	message string,
) error {
	if err := domain.ValidateTransition(current, next); err != nil {
		return fmt.Errorf("validate step transition: %w", err)
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.TaskStep{}).
			Where("id = ? AND task_id = ? AND status = ?", stepID, taskID, current).
			Update("status", next)
		if result.Error != nil {
			return fmt.Errorf("update step status: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}

		logEntry := domain.ExecutionLog{
			TaskID:  taskID,
			StepID:  &stepID,
			Level:   level,
			Message: message,
		}
		if err := tx.Create(&logEntry).Error; err != nil {
			return fmt.Errorf("create step transition log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("transition step: %w", err)
	}
	return nil
}
