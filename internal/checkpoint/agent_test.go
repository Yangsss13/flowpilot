package checkpoint

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Yangsss13/flowpilot/internal/agent"
)

func TestMiniKVStorePersistsAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "checkpoints")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	want := Agent{
		TaskID:        42,
		Phase:         PhaseReady,
		NextIteration: 3,
		State: agent.AgentState{
			Goal: "summarize",
			Plan: agent.Plan{Steps: []agent.PlanStep{{
				ID: "search", Tool: agent.ToolRAGQuery, Input: json.RawMessage(`{"query":"Go"}`),
			}}},
			Observations: []agent.Observation{{StepID: "search", Output: json.RawMessage(`{"hits":1}`)}},
			ReplanCount:  1,
		},
	}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer store.Close()
	got, ok, err := store.Load(context.Background(), want.TaskID)
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v error=%v", ok, err)
	}
	if got.NextIteration != want.NextIteration || got.State.Goal != want.State.Goal || got.State.ReplanCount != 1 || len(got.State.Observations) != 1 {
		t.Fatalf("loaded checkpoint = %#v", got)
	}
	if err := store.Delete(context.Background(), want.TaskID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok, err := store.Load(context.Background(), want.TaskID); err != nil || ok {
		t.Fatalf("Load() after delete ok=%v error=%v", ok, err)
	}
}
