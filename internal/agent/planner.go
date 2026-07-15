package agent

import (
	"context"
	"fmt"
	"strings"
)

type Planner struct {
	provider  ChatProvider
	validator *Validator
	tools     []ToolDefinition
}

func NewPlanner(provider ChatProvider, tools []ToolDefinition, validator *Validator) *Planner {
	return &Planner{provider: provider, tools: tools, validator: validator}
}

func (p *Planner) CreatePlan(ctx context.Context, goal string) (Plan, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Plan{}, fmt.Errorf("goal is required")
	}
	plan, err := p.provider.Plan(ctx, goal, p.tools)
	if err != nil {
		return Plan{}, fmt.Errorf("generate plan: %w", err)
	}
	if err := p.validator.ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p *Planner) Decide(ctx context.Context, state AgentState) (Decision, error) {
	decision, err := p.provider.Decide(ctx, state)
	if err != nil {
		return Decision{}, fmt.Errorf("generate decision: %w", err)
	}
	if err := p.validator.ValidateDecision(decision, state); err != nil {
		return Decision{}, err
	}
	return decision, nil
}
