package domain

import (
	"encoding/json"
	"time"
)

// Task is the workflow definition and its current overall state.
type Task struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"size:100;not null" json:"name"`
	Description string     `gorm:"size:500;not null;default:''" json:"description"`
	Status      Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Steps       []TaskStep `gorm:"foreignKey:TaskID" json:"steps,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskStep is one ordered action in a task. Steps within one task are run
// sequentially in StepOrder during v1.
type TaskStep struct {
	ID            uint64          `gorm:"primaryKey" json:"id"`
	TaskID        uint64          `gorm:"not null;uniqueIndex:idx_task_step_order" json:"task_id"`
	Name          string          `gorm:"size:100;not null" json:"name"`
	StepOrder     int             `gorm:"not null;uniqueIndex:idx_task_step_order" json:"step_order"`
	ActionType    string          `gorm:"size:30;not null" json:"action_type"`
	ActionPayload json.RawMessage `gorm:"type:json;not null" json:"action_payload"`
	Status        Status          `gorm:"type:varchar(20);not null;index" json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ExecutionLog records either a task-level event or a step-level event.
// StepID is nil for task-level events such as "task started".
type ExecutionLog struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TaskID    uint64    `gorm:"not null;index" json:"task_id"`
	StepID    *uint64   `gorm:"index" json:"step_id,omitempty"`
	Level     string    `gorm:"size:10;not null;index" json:"level"`
	Message   string    `gorm:"size:1000;not null" json:"message"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
