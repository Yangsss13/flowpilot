package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"minikvx-agent/internal/action"
	"minikvx-agent/internal/domain"
)

type StepExecutor struct{}

func NewStepExecutor() *StepExecutor {
	return &StepExecutor{}
}

func (e *StepExecutor) Execute(ctx context.Context, step domain.TaskStep) error {
	switch step.ActionType {
	case "sleep":
		return executeSleep(ctx, step.ActionPayload)
	case "http_mock":
		return executeHTTPMock(step.ActionPayload)
	case "shell_mock":
		return executeShellMock(step.ActionPayload)
	default:
		return fmt.Errorf("unsupported action type %q", step.ActionType)
	}
}

func executeSleep(ctx context.Context, payload json.RawMessage) error {
	input, err := action.ParseSleep(payload)
	if err != nil {
		return err
	}
	duration := time.Duration(input.DurationMS) * time.Millisecond

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("sleep interrupted: %w", ctx.Err())
	}
}

func executeHTTPMock(payload json.RawMessage) error {
	input, err := action.ParseHTTPMock(payload)
	if err != nil {
		return err
	}
	if input.Status >= 400 {
		return fmt.Errorf("http_mock returned status %d", input.Status)
	}
	return nil
}

func executeShellMock(payload json.RawMessage) error {
	input, err := action.ParseShellMock(payload)
	if err != nil {
		return err
	}
	if input.ExitCode != 0 {
		return fmt.Errorf("shell_mock exited with code %d", input.ExitCode)
	}
	return nil
}
