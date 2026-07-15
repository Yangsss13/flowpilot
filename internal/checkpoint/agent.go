package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	minikv "github.com/Yangsss13/miniKV"

	"github.com/Yangsss13/flowpilot/internal/agent"
)

const CurrentVersion = 1

type Phase string

const (
	PhaseReady     Phase = "ready"
	PhaseExecuting Phase = "executing"
)

// Agent records the durable control state needed to classify a restart as
// safe to resume or ambiguous because an external tool may have run.
type Agent struct {
	Version         int              `json:"version"`
	TaskID          uint64           `json:"task_id"`
	Phase           Phase            `json:"phase"`
	CurrentStepID   uint64           `json:"current_step_id,omitempty"`
	CurrentStepName string           `json:"current_step_name,omitempty"`
	NextIteration   int              `json:"next_iteration"`
	State           agent.AgentState `json:"state"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type Store interface {
	Save(ctx context.Context, value Agent) error
	Load(ctx context.Context, taskID uint64) (Agent, bool, error)
	Delete(ctx context.Context, taskID uint64) error
}

type MiniKVStore struct {
	store *minikv.Store
}

func Open(dir string) (*MiniKVStore, error) {
	store, err := minikv.OpenWithOptions(dir, minikv.Options{
		CleanupInterval:     time.Minute,
		AutoCompactMinBytes: 64 * 1024 * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("open MiniKV checkpoint store: %w", err)
	}
	return &MiniKVStore{store: store}, nil
}

func (s *MiniKVStore) Save(ctx context.Context, value Agent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value.TaskID == 0 {
		return fmt.Errorf("checkpoint task ID must be positive")
	}
	if value.Phase != PhaseReady && value.Phase != PhaseExecuting {
		return fmt.Errorf("unsupported checkpoint phase %q", value.Phase)
	}
	value.Version = CurrentVersion
	value.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode agent checkpoint: %w", err)
	}
	if err := s.store.Set(agentKey(value.TaskID), string(payload)); err != nil {
		return fmt.Errorf("save agent checkpoint: %w", err)
	}
	return nil
}

func (s *MiniKVStore) Load(ctx context.Context, taskID uint64) (Agent, bool, error) {
	if err := ctx.Err(); err != nil {
		return Agent{}, false, err
	}
	payload, ok := s.store.Get(agentKey(taskID))
	if !ok {
		return Agent{}, false, nil
	}
	var value Agent
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return Agent{}, false, fmt.Errorf("decode agent checkpoint: %w", err)
	}
	if value.Version != CurrentVersion {
		return Agent{}, false, fmt.Errorf("unsupported agent checkpoint version %d", value.Version)
	}
	if value.TaskID != taskID {
		return Agent{}, false, fmt.Errorf("agent checkpoint task ID %d does not match %d", value.TaskID, taskID)
	}
	if value.Phase != PhaseReady && value.Phase != PhaseExecuting {
		return Agent{}, false, fmt.Errorf("unsupported checkpoint phase %q", value.Phase)
	}
	return value, true, nil
}

func (s *MiniKVStore) Delete(ctx context.Context, taskID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.store.Delete(agentKey(taskID)); err != nil {
		return fmt.Errorf("delete agent checkpoint: %w", err)
	}
	return nil
}

func (s *MiniKVStore) Close() error {
	return s.store.Close()
}

func agentKey(taskID uint64) string {
	return fmt.Sprintf("agent:checkpoint:%d", taskID)
}
