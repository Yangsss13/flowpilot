package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

var ErrNotFound = errors.New("repository: not found")

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) error
	List(ctx context.Context) ([]domain.Task, error)
	GetByID(ctx context.Context, id uint64) (*domain.Task, error)
}

// List returns lightweight task summaries. Associations are intentionally not
// preloaded because the list endpoint does not need every task's steps.
func (r *GormTaskRepository) List(ctx context.Context) ([]domain.Task, error) {
	var tasks []domain.Task
	if err := r.db.WithContext(ctx).
		Order("created_at DESC, id DESC").
		Limit(100).
		Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

// GetByID returns one task and its steps in business execution order.
func (r *GormTaskRepository) GetByID(ctx context.Context, id uint64) (*domain.Task, error) {
	var task domain.Task
	err := r.db.WithContext(ctx).
		Preload("Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("step_order ASC")
		}).
		First(&task, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task by id: %w", err)
	}
	return &task, nil
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
