package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"minikvx-agent/internal/domain"
)

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) error
}

type GormTaskRepository struct {
	db *gorm.DB
}

func NewGormTaskRepository(db *gorm.DB) *GormTaskRepository {
	return &GormTaskRepository{db: db}
}

// Create persists a task and all of its steps as one atomic operation.
func (r *GormTaskRepository) Create(ctx context.Context, task *domain.Task) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit(clause.Associations).Create(task).Error; err != nil {
			return fmt.Errorf("create task: %w", err)
		}

		for i := range task.Steps {
			task.Steps[i].TaskID = task.ID
		}
		if err := tx.Create(&task.Steps).Error; err != nil {
			return fmt.Errorf("create task steps: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("create task transaction: %w", err)
	}
	return nil
}
