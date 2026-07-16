package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

var ErrNotFound = errors.New("repository: not found")

type TaskRepository interface {
	Create(ctx context.Context, task *domain.Task) error
	List(ctx context.Context, query TaskListQuery) (TaskListPage, error)
	Stats(ctx context.Context) (TaskStats, error)
	GetByID(ctx context.Context, id uint64) (*domain.Task, error)
	DeleteInactive(ctx context.Context, id uint64) error
	ReserveForQueue(ctx context.Context, id uint64, taskType domain.TaskType) (domain.Status, error)
	ReleaseQueueReservation(ctx context.Context, id uint64, previous domain.Status) error
}

type TaskListQuery struct {
	Offset   int
	Limit    int
	TaskType domain.TaskType
	Status   domain.Status
	Search   string
}

type TaskListItem struct {
	ID          uint64
	Name        string
	Description string
	TaskType    domain.TaskType
	Status      domain.Status
	StepCount   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TaskListPage struct {
	Items []TaskListItem
	Total int64
}

type TaskStats struct {
	Total    int64
	ByStatus map[domain.Status]int64
	ByType   map[domain.TaskType]int64
}

// List returns paginated summaries and computes step_count in the same query.
func (r *GormTaskRepository) List(ctx context.Context, query TaskListQuery) (TaskListPage, error) {
	base := func() *gorm.DB {
		db := r.db.WithContext(ctx).Model(&domain.Task{})
		if query.TaskType != "" {
			db = db.Where("task_type = ?", query.TaskType)
		}
		if query.Status != "" {
			db = db.Where("status = ?", query.Status)
		}
		if search := strings.TrimSpace(query.Search); search != "" {
			pattern := "%" + search + "%"
			db = db.Where("name LIKE ? OR description LIKE ?", pattern, pattern)
		}
		return db
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return TaskListPage{}, fmt.Errorf("count tasks: %w", err)
	}
	items := make([]TaskListItem, 0)
	if err := base().
		Select("tasks.id, tasks.name, tasks.description, tasks.task_type, tasks.status, tasks.created_at, tasks.updated_at, (SELECT COUNT(*) FROM task_steps WHERE task_steps.task_id = tasks.id) AS step_count").
		Order("created_at DESC, id DESC").
		Offset(query.Offset).
		Limit(query.Limit).
		Scan(&items).Error; err != nil {
		return TaskListPage{}, fmt.Errorf("list tasks: %w", err)
	}
	return TaskListPage{Items: items, Total: total}, nil
}

func (r *GormTaskRepository) Stats(ctx context.Context) (TaskStats, error) {
	result := TaskStats{ByStatus: make(map[domain.Status]int64), ByType: make(map[domain.TaskType]int64)}
	if err := r.db.WithContext(ctx).Model(&domain.Task{}).Count(&result.Total).Error; err != nil {
		return TaskStats{}, fmt.Errorf("count task stats: %w", err)
	}
	var statusRows []struct {
		Status domain.Status
		Count  int64
	}
	if err := r.db.WithContext(ctx).Model(&domain.Task{}).
		Select("status, COUNT(*) AS count").Group("status").Scan(&statusRows).Error; err != nil {
		return TaskStats{}, fmt.Errorf("count tasks by status: %w", err)
	}
	for _, row := range statusRows {
		result.ByStatus[row.Status] = row.Count
	}
	var typeRows []struct {
		TaskType domain.TaskType
		Count    int64
	}
	if err := r.db.WithContext(ctx).Model(&domain.Task{}).
		Select("task_type, COUNT(*) AS count").Group("task_type").Scan(&typeRows).Error; err != nil {
		return TaskStats{}, fmt.Errorf("count tasks by type: %w", err)
	}
	for _, row := range typeRows {
		result.ByType[row.TaskType] = row.Count
	}
	return result, nil
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

// DeleteInactive removes an unqueued task and its dependent records atomically.
// Locking the task row prevents a concurrent queue reservation from racing the
// deletion decision.
func (r *GormTaskRepository) DeleteInactive(ctx context.Context, id uint64) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status").First(&task, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load task for deletion: %w", err)
		}
		if task.Status == domain.StatusQueued || task.Status == domain.StatusRunning {
			return ErrStateConflict
		}
		if err := tx.Where("task_id = ?", id).Delete(&domain.ExecutionLog{}).Error; err != nil {
			return fmt.Errorf("delete task logs: %w", err)
		}
		if err := tx.Where("task_id = ?", id).Delete(&domain.TaskStep{}).Error; err != nil {
			return fmt.Errorf("delete task steps: %w", err)
		}
		if result := tx.Delete(&domain.Task{}, id); result.Error != nil {
			return fmt.Errorf("delete task: %w", result.Error)
		} else if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete task transaction: %w", err)
	}
	return nil
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

func (r *GormTaskRepository) ReserveForQueue(ctx context.Context, id uint64, taskType domain.TaskType) (domain.Status, error) {
	var previous domain.Status
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "task_type", "status").First(&task, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load task for queue reservation: %w", err)
		}
		if task.TaskType != taskType || (task.Status != domain.StatusPending && task.Status != domain.StatusFailed) {
			return ErrStateConflict
		}
		previous = task.Status
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND status = ?", id, previous).
			Update("status", domain.StatusQueued)
		if result.Error != nil {
			return fmt.Errorf("reserve task for queue: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		return tx.Create(&domain.ExecutionLog{
			TaskID: id, Level: domain.LogLevelInfo, Message: "task queued",
		}).Error
	})
	if err != nil {
		return "", fmt.Errorf("queue task transaction: %w", err)
	}
	return previous, nil
}

func (r *GormTaskRepository) ReleaseQueueReservation(ctx context.Context, id uint64, previous domain.Status) error {
	if previous != domain.StatusPending && previous != domain.StatusFailed {
		return fmt.Errorf("invalid queue reservation previous status %s", previous)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Task{}).
			Where("id = ? AND status = ?", id, domain.StatusQueued).
			Update("status", previous)
		if result.Error != nil {
			return fmt.Errorf("release queue reservation: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrStateConflict
		}
		return tx.Create(&domain.ExecutionLog{
			TaskID: id, Level: domain.LogLevelWarn, Message: "task queue publication failed; reservation released",
		}).Error
	})
	if err != nil {
		return fmt.Errorf("release queue reservation transaction: %w", err)
	}
	return nil
}
