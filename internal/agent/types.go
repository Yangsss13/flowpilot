package agent

import (
	"context"
	"encoding/json"
)

const MaxPlanSteps = 5
const MaxReplans = 2

type ToolName string

const (
	ToolRAGQuery    ToolName = "rag_query"
	ToolHTTPRequest ToolName = "http_request"
)

type ToolDefinition struct {
	Name        ToolName        `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Plan struct {
	Steps []PlanStep `json:"steps"`
}

type PlanStep struct {
	ID        string          `json:"id"`
	Tool      ToolName        `json:"tool"`
	Input     json.RawMessage `json:"input"`
	DependsOn []string        `json:"depends_on"`
}

type Observation struct {
	StepID string          `json:"step_id"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error,omitempty"`
}

type AgentState struct {
	Goal         string        `json:"goal"`
	Plan         Plan          `json:"plan"`
	Observations []Observation `json:"observations"`
	ReplanCount  int           `json:"replan_count"`
}

type DecisionAction string

const (
	DecisionContinue DecisionAction = "continue"
	DecisionReplan   DecisionAction = "replan"
	DecisionFinish   DecisionAction = "finish"
	DecisionFail     DecisionAction = "fail"
)

type Decision struct {
	Action      DecisionAction `json:"action"`
	NextStepID  string         `json:"next_step_id,omitempty"`
	FinalAnswer string         `json:"final_answer,omitempty"`
	Reason      string         `json:"reason,omitempty"`
}

type ChatProvider interface {
	Plan(ctx context.Context, goal string, tools []ToolDefinition) (Plan, error)
	Decide(ctx context.Context, state AgentState) (Decision, error)
}

func DefaultToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolRAGQuery,
			Description: "Search the imported knowledge base for relevant text passages.",
			InputSchema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}},"additionalProperties":false}`),
		},
		{
			Name:        ToolHTTPRequest,
			Description: "Call an HTTP endpoint allowed by the server-side allowlist.",
			InputSchema: json.RawMessage(`{"type":"object","required":["method","url"],"properties":{"method":{"type":"string","enum":["GET","POST"]},"url":{"type":"string"},"body":{}},"additionalProperties":false}`),
		},
	}
}
