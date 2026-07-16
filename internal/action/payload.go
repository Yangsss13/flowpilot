package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxMockSleep = 30 * time.Second
const MaxRAGQueryRunes = 2000
const MaxSummaryInstructionRunes = 2000

type SleepPayload struct {
	DurationMS int `json:"duration_ms"`
}

type HTTPMockPayload struct {
	Status int `json:"status"`
}

type ShellMockPayload struct {
	ExitCode int `json:"exit_code"`
}

type RAGQueryPayload struct {
	Query    string  `json:"query"`
	TopK     int     `json:"top_k,omitempty"`
	MinScore float64 `json:"min_score,omitempty"`
}

type LLMSummarizePayload struct {
	Instruction string `json:"instruction"`
}

type Capabilities struct {
	RAGQuery     bool
	LLMSummarize bool
}

func Validate(actionType string, payload json.RawMessage, capabilities ...Capabilities) error {
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
	case "rag_query":
		if len(capabilities) == 0 || !capabilities[0].RAGQuery {
			return fmt.Errorf("rag_query is not available")
		}
		_, err := ParseRAGQuery(payload)
		return err
	case "llm_summarize":
		if len(capabilities) == 0 || !capabilities[0].LLMSummarize {
			return fmt.Errorf("llm_summarize is not available")
		}
		_, err := ParseLLMSummarize(payload)
		return err
	default:
		return fmt.Errorf("unsupported action type %q", actionType)
	}
}

func ParseLLMSummarize(payload json.RawMessage) (LLMSummarizePayload, error) {
	var input LLMSummarizePayload
	if err := decodeStrictPayload(payload, &input); err != nil {
		return LLMSummarizePayload{}, fmt.Errorf("decode llm_summarize payload: %w", err)
	}
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.Instruction == "" {
		return LLMSummarizePayload{}, fmt.Errorf("llm_summarize instruction is required")
	}
	if utf8.RuneCountInString(input.Instruction) > MaxSummaryInstructionRunes {
		return LLMSummarizePayload{}, fmt.Errorf("llm_summarize instruction must not exceed %d characters", MaxSummaryInstructionRunes)
	}
	return input, nil
}

func ParseRAGQuery(payload json.RawMessage) (RAGQueryPayload, error) {
	var input RAGQueryPayload
	if err := decodeStrictPayload(payload, &input); err != nil {
		return RAGQueryPayload{}, fmt.Errorf("decode rag_query payload: %w", err)
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return RAGQueryPayload{}, fmt.Errorf("rag_query query is required")
	}
	if utf8.RuneCountInString(input.Query) > MaxRAGQueryRunes {
		return RAGQueryPayload{}, fmt.Errorf("rag_query query must not exceed %d characters", MaxRAGQueryRunes)
	}
	if input.TopK < 0 || input.TopK > 10 {
		return RAGQueryPayload{}, fmt.Errorf("rag_query top_k must be between 1 and 10 when provided")
	}
	if input.MinScore < 0 || input.MinScore > 1 {
		return RAGQueryPayload{}, fmt.Errorf("rag_query min_score must be between 0 and 1")
	}
	return input, nil
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
