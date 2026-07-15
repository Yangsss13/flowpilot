package domain

import (
	"encoding/json"
	"time"
)

type LogLevel string

type TaskType string

const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

const (
	TaskTypeWorkflow TaskType = "workflow"
	TaskTypeAgent    TaskType = "agent"
)

// Task is a workflow or agent definition and its current overall state.
type Task struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"size:100;not null" json:"name"`
	Description string     `gorm:"size:500;not null;default:''" json:"description"`
	TaskType    TaskType   `gorm:"column:task_type;type:varchar(20);not null;default:'workflow';index" json:"task_type"`
	Status      Status     `gorm:"type:varchar(20);not null;index" json:"status"`
	Steps       []TaskStep `gorm:"foreignKey:TaskID" json:"steps,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskStep is one action in a task. Workflow steps run sequentially in
// StepOrder; agent steps may additionally reference dependencies.
type TaskStep struct {
	ID            uint64          `gorm:"primaryKey" json:"id"`
	TaskID        uint64          `gorm:"not null;uniqueIndex:idx_task_step_order" json:"task_id"`
	Name          string          `gorm:"size:100;not null" json:"name"`
	StepOrder     int             `gorm:"not null;uniqueIndex:idx_task_step_order" json:"step_order"`
	ActionType    string          `gorm:"size:30;not null" json:"action_type"`
	ActionPayload json.RawMessage `gorm:"type:json;not null" json:"action_payload"`
	DependsOn     json.RawMessage `gorm:"type:json" json:"depends_on,omitempty"`
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
	Level     LogLevel  `gorm:"type:varchar(10);not null;index" json:"level"`
	Message   string    `gorm:"size:1000;not null" json:"message"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
