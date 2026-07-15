package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var ErrInvalidPlan = errors.New("invalid agent plan")
var ErrInvalidDecision = errors.New("invalid agent decision")

var stepIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

type Validator struct {
	maxSteps int
	tools    map[ToolName]struct{}
}

func NewValidator(tools []ToolDefinition, maxSteps int) (*Validator, error) {
	if maxSteps <= 0 {
		return nil, fmt.Errorf("maximum plan steps must be positive")
	}
	allowed := make(map[ToolName]struct{}, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		if _, exists := allowed[tool.Name]; exists {
			return nil, fmt.Errorf("duplicate tool definition %q", tool.Name)
		}
		allowed[tool.Name] = struct{}{}
	}
	return &Validator{maxSteps: maxSteps, tools: allowed}, nil
}

func ParsePlanJSON(content []byte) (Plan, error) {
	var plan Plan
	if err := decodeStrictJSON(content, &plan); err != nil {
		return Plan{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalidPlan, err)
	}
	return plan, nil
}

func ParseDecisionJSON(content []byte) (Decision, error) {
	var decision Decision
	if err := decodeStrictJSON(content, &decision); err != nil {
		return Decision{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalidDecision, err)
	}
	return decision, nil
}

func (v *Validator) ValidatePlan(plan Plan) error {
	if len(plan.Steps) == 0 {
		return fmt.Errorf("%w: at least one step is required", ErrInvalidPlan)
	}
	if len(plan.Steps) > v.maxSteps {
		return fmt.Errorf("%w: step count %d exceeds maximum %d", ErrInvalidPlan, len(plan.Steps), v.maxSteps)
	}

	steps := make(map[string]PlanStep, len(plan.Steps))
	for index, step := range plan.Steps {
		if !stepIDPattern.MatchString(step.ID) {
			return fmt.Errorf("%w: step %d has invalid id %q", ErrInvalidPlan, index+1, step.ID)
		}
		if _, exists := steps[step.ID]; exists {
			return fmt.Errorf("%w: duplicate step id %q", ErrInvalidPlan, step.ID)
		}
		if _, allowed := v.tools[step.Tool]; !allowed {
			return fmt.Errorf("%w: step %q uses unknown tool %q", ErrInvalidPlan, step.ID, step.Tool)
		}
		if err := validateToolInput(step.Tool, step.Input); err != nil {
			return fmt.Errorf("%w: step %q input: %v", ErrInvalidPlan, step.ID, err)
		}
		steps[step.ID] = step
	}

	for _, step := range plan.Steps {
		seenDependencies := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return fmt.Errorf("%w: step %q depends on itself", ErrInvalidPlan, step.ID)
			}
			if _, exists := steps[dependency]; !exists {
				return fmt.Errorf("%w: step %q depends on unknown step %q", ErrInvalidPlan, step.ID, dependency)
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return fmt.Errorf("%w: step %q repeats dependency %q", ErrInvalidPlan, step.ID, dependency)
			}
			seenDependencies[dependency] = struct{}{}
		}
	}
	if err := validateAcyclic(steps); err != nil {
		return err
	}
	return nil
}

func (v *Validator) ValidateDecision(decision Decision, state AgentState) error {
	switch decision.Action {
	case DecisionContinue:
		if strings.TrimSpace(decision.NextStepID) == "" {
			return fmt.Errorf("%w: continue requires next_step_id", ErrInvalidDecision)
		}
		if !planHasStep(state.Plan, decision.NextStepID) {
			return fmt.Errorf("%w: next_step_id %q is not in the plan", ErrInvalidDecision, decision.NextStepID)
		}
	case DecisionReplan:
		if state.ReplanCount >= MaxReplans {
			return fmt.Errorf("%w: replan limit %d reached", ErrInvalidDecision, MaxReplans)
		}
		if strings.TrimSpace(decision.Reason) == "" {
			return fmt.Errorf("%w: replan requires reason", ErrInvalidDecision)
		}
	case DecisionFinish:
		if strings.TrimSpace(decision.FinalAnswer) == "" {
			return fmt.Errorf("%w: finish requires final_answer", ErrInvalidDecision)
		}
	case DecisionFail:
		if strings.TrimSpace(decision.Reason) == "" {
			return fmt.Errorf("%w: fail requires reason", ErrInvalidDecision)
		}
	default:
		return fmt.Errorf("%w: unknown action %q", ErrInvalidDecision, decision.Action)
	}
	return nil
}

type ragQueryInput struct {
	Query string `json:"query"`
}

type httpRequestInput struct {
	Method string          `json:"method"`
	URL    string          `json:"url"`
	Body   json.RawMessage `json:"body,omitempty"`
}

func validateToolInput(tool ToolName, input json.RawMessage) error {
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}
	switch tool {
	case ToolRAGQuery:
		var value ragQueryInput
		if err := decodeStrictJSON(input, &value); err != nil {
			return err
		}
		if strings.TrimSpace(value.Query) == "" {
			return fmt.Errorf("query is required")
		}
	case ToolHTTPRequest:
		var value httpRequestInput
		if err := decodeStrictJSON(input, &value); err != nil {
			return err
		}
		value.Method = strings.ToUpper(strings.TrimSpace(value.Method))
		if value.Method != "GET" && value.Method != "POST" {
			return fmt.Errorf("method must be GET or POST")
		}
		target, err := url.Parse(value.URL)
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
			return fmt.Errorf("url must be an absolute HTTP or HTTPS URL")
		}
		if target.User != nil {
			return fmt.Errorf("url must not contain credentials")
		}
	default:
		return fmt.Errorf("unknown tool %q", tool)
	}
	return nil
}

func validateAcyclic(steps map[string]PlanStep) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(steps))
	var visit func(string) error
	visit = func(id string) error {
		switch states[id] {
		case visiting:
			return fmt.Errorf("%w: dependency cycle includes step %q", ErrInvalidPlan, id)
		case visited:
			return nil
		}
		states[id] = visiting
		for _, dependency := range steps[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		states[id] = visited
		return nil
	}
	for id := range steps {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func planHasStep(plan Plan, id string) bool {
	for _, step := range plan.Steps {
		if step.ID == id {
			return true
		}
	}
	return false
}

func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
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
