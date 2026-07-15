package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

func (r *GormExecutionRepository) CompleteAgentStep(
	ctx context.Context,
	taskID, stepID uint64,
	next domain.Status,
	observation agent.Observation,
) error {
	if err := domain.ValidateTransition(domain.StatusRunning, next); err != nil {
		return fmt.Errorf("validate agent step transition: %w", err)
	}
	payload, err := json.Marshal(observation)
	if err != nil {
		return fmt.Errorf("encode observation: %w", err)
	}
	level := domain.LogLevelInfo
	if next == domain.StatusFailed {
		level = domain.LogLevelError
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.TaskStep{}).
			Where("id = ? AND task_id = ? AND status = ?", stepID, taskID, domain.StatusRunning).
			Updates(map[string]any{"status": next, "observation": payload})
		if result.Error != nil {
			return fmt.Errorf("update agent step: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		message := "agent step succeeded"
		if next == domain.StatusFailed {
			message = "agent step failed: " + observation.Error
		}
		message = truncateRunes(message, 1000)
		if err := tx.Create(&domain.ExecutionLog{TaskID: taskID, StepID: &stepID, Level: level, Message: message}).Error; err != nil {
			return fmt.Errorf("create agent step log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete agent step: %w", err)
	}
	return nil
}

func (r *GormExecutionRepository) CompleteAgentTask(
	ctx context.Context,
	taskID uint64,
	next domain.Status,
	resultText, message string,
) error {
	if err := domain.ValidateTransition(domain.StatusRunning, next); err != nil {
		return fmt.Errorf("validate agent task transition: %w", err)
	}
	level := domain.LogLevelInfo
	if next == domain.StatusFailed {
		level = domain.LogLevelError
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND task_type = ? AND status = ?", taskID, domain.TaskTypeAgent, domain.StatusRunning).
			Updates(map[string]any{"status": next, "result": resultText})
		if result.Error != nil {
			return fmt.Errorf("update agent task: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		if err := tx.Create(&domain.ExecutionLog{TaskID: taskID, Level: level, Message: truncateRunes(message, 1000)}).Error; err != nil {
			return fmt.Errorf("create agent task log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete agent task: %w", err)
	}
	return nil
}

// InterruptAgentTask atomically closes a task that was recovered while a tool
// might have been executing. The external side effect cannot be rolled back or
// proven absent, so automatic replay would be unsafe.
func (r *GormExecutionRepository) InterruptAgentTask(
	ctx context.Context,
	taskID, stepID uint64,
	observation agent.Observation,
	message string,
) error {
	payload, err := json.Marshal(observation)
	if err != nil {
		return fmt.Errorf("encode interruption observation: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if stepID != 0 {
			result := tx.Model(&domain.TaskStep{}).
				Where("id = ? AND task_id = ? AND status = ?", stepID, taskID, domain.StatusRunning).
				Updates(map[string]any{"status": domain.StatusFailed, "observation": payload})
			if result.Error != nil {
				return fmt.Errorf("interrupt agent step: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrStateConflict
			}
			if err := tx.Create(&domain.ExecutionLog{
				TaskID: taskID, StepID: &stepID, Level: domain.LogLevelError, Message: truncateRunes(message, 1000),
			}).Error; err != nil {
				return fmt.Errorf("create interrupted step log: %w", err)
			}
		}
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND task_type = ? AND status = ?", taskID, domain.TaskTypeAgent, domain.StatusRunning).
			Updates(map[string]any{"status": domain.StatusFailed, "result": message})
		if result.Error != nil {
			return fmt.Errorf("interrupt agent task: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		if err := tx.Create(&domain.ExecutionLog{
			TaskID: taskID, Level: domain.LogLevelError, Message: truncateRunes(message, 1000),
		}).Error; err != nil {
			return fmt.Errorf("create interrupted task log: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("interrupt agent task transaction: %w", err)
	}
	return nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (r *GormExecutionRepository) ReplaceAgentPlan(
	ctx context.Context,
	taskID uint64,
	plan agent.Plan,
	replanCount int,
) ([]domain.TaskStep, error) {
	steps := make([]domain.TaskStep, len(plan.Steps))
	for index, planStep := range plan.Steps {
		dependsOn, err := json.Marshal(planStep.DependsOn)
		if err != nil {
			return nil, fmt.Errorf("encode dependencies for step %q: %w", planStep.ID, err)
		}
		steps[index] = domain.TaskStep{
			TaskID:        taskID,
			Name:          planStep.ID,
			StepOrder:     index + 1,
			ActionType:    string(planStep.Tool),
			ActionPayload: append(json.RawMessage(nil), planStep.Input...),
			DependsOn:     dependsOn,
			Status:        domain.StatusPending,
		}
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND task_type = ? AND status = ?", taskID, domain.TaskTypeAgent, domain.StatusRunning).
			Update("replan_count", replanCount)
		if result.Error != nil {
			return fmt.Errorf("update replan count: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&domain.TaskStep{}).Error; err != nil {
			return fmt.Errorf("delete previous agent steps: %w", err)
		}
		if err := tx.Create(&steps).Error; err != nil {
			return fmt.Errorf("create replacement agent steps: %w", err)
		}
		message := fmt.Sprintf("agent replanned (%d/%d)", replanCount, agent.MaxReplans)
		if err := tx.Create(&domain.ExecutionLog{TaskID: taskID, Level: domain.LogLevelWarn, Message: message}).Error; err != nil {
			return fmt.Errorf("create replan log: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("replace agent plan: %w", err)
	}
	return steps, nil
}
