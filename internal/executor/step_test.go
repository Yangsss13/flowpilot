package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"minikvx-agent/internal/domain"
)

func TestStepExecutorExecute(t *testing.T) {
	tests := []struct {
		name    string
		step    domain.TaskStep
		wantErr bool
	}{
		{name: "sleep succeeds", step: step("sleep", `{"duration_ms":1}`)},
		{name: "sleep rejects zero", step: step("sleep", `{"duration_ms":0}`), wantErr: true},
		{name: "http mock succeeds", step: step("http_mock", `{"status":200}`)},
		{name: "http mock fails", step: step("http_mock", `{"status":500}`), wantErr: true},
		{name: "http mock rejects invalid status", step: step("http_mock", `{"status":999}`), wantErr: true},
		{name: "shell mock succeeds", step: step("shell_mock", `{"exit_code":0}`)},
		{name: "shell mock fails", step: step("shell_mock", `{"exit_code":1}`), wantErr: true},
		{name: "unsupported action", step: step("real_shell", `{}`), wantErr: true},
		{name: "invalid JSON", step: step("sleep", `{`), wantErr: true},
	}

	executor := NewStepExecutor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Execute(context.Background(), tt.step)
			if tt.wantErr && err == nil {
				t.Fatal("Execute() returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Execute() returned error: %v", err)
			}
		})
	}
}

func TestStepExecutorSleepHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	err := NewStepExecutor().Execute(ctx, step("sleep", `{"duration_ms":30000}`))
	if err == nil {
		t.Fatal("Execute() returned nil, want cancellation error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled sleep took %s, want under 1s", elapsed)
	}
}

func step(actionType, payload string) domain.TaskStep {
	return domain.TaskStep{
		ActionType:    actionType,
		ActionPayload: json.RawMessage(payload),
	}
}
