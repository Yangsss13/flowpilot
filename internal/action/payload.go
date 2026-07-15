package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const MaxMockSleep = 30 * time.Second

type SleepPayload struct {
	DurationMS int `json:"duration_ms"`
}

type HTTPMockPayload struct {
	Status int `json:"status"`
}

type ShellMockPayload struct {
	ExitCode int `json:"exit_code"`
}

func Validate(actionType string, payload json.RawMessage) error {
	switch actionType {
	case "sleep":
		_, err := ParseSleep(payload)
		return err
	case "http_mock":
		_, err := ParseHTTPMock(payload)
		return err
	case "shell_mock":
		_, err := ParseShellMock(payload)
		return err
	default:
		return fmt.Errorf("unsupported action type %q", actionType)
	}
}

func ParseSleep(payload json.RawMessage) (SleepPayload, error) {
	var input SleepPayload
	if err := decodeStrictPayload(payload, &input); err != nil {
		return SleepPayload{}, fmt.Errorf("decode sleep payload: %w", err)
	}
	duration := time.Duration(input.DurationMS) * time.Millisecond
	if duration <= 0 || duration > MaxMockSleep {
		return SleepPayload{}, fmt.Errorf("sleep duration_ms must be between 1 and %d", MaxMockSleep.Milliseconds())
	}
	return input, nil
}

func ParseHTTPMock(payload json.RawMessage) (HTTPMockPayload, error) {
	var input HTTPMockPayload
	if err := decodeStrictPayload(payload, &input); err != nil {
		return HTTPMockPayload{}, fmt.Errorf("decode http_mock payload: %w", err)
	}
	if input.Status < 100 || input.Status > 599 {
		return HTTPMockPayload{}, fmt.Errorf("http_mock status must be between 100 and 599")
	}
	return input, nil
}

func ParseShellMock(payload json.RawMessage) (ShellMockPayload, error) {
	var input ShellMockPayload
	if err := decodeStrictPayload(payload, &input); err != nil {
		return ShellMockPayload{}, fmt.Errorf("decode shell_mock payload: %w", err)
	}
	if input.ExitCode < 0 || input.ExitCode > 255 {
		return ShellMockPayload{}, fmt.Errorf("shell_mock exit_code must be between 0 and 255")
	}
	return input, nil
}

func decodeStrictPayload(payload json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
