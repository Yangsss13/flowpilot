package agent

import (
	"context"
	"errors"
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
	plan, err := p.provider.Plan(ctx, PlanRequest{Goal: goal}, p.tools)
	if err != nil {
		return Plan{}, fmt.Errorf("generate plan: %w", err)
	}
	if err := p.validator.ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p *Planner) Replan(ctx context.Context, state AgentState, reason string) (Plan, error) {
	reason = strings.TrimSpace(reason)
	if state.ReplanCount >= MaxReplans {
		return Plan{}, fmt.Errorf("replan limit %d reached", MaxReplans)
	}
	if reason == "" {
		return Plan{}, fmt.Errorf("replan reason is required")
	}
	previous := state.Plan
	plan, err := p.provider.Plan(ctx, PlanRequest{
		Goal:         state.Goal,
		PreviousPlan: &previous,
		Observations: state.Observations,
		ReplanReason: reason,
		ReplanCount:  state.ReplanCount + 1,
	}, p.tools)
	if err != nil {
		return Plan{}, fmt.Errorf("generate replacement plan: %w", err)
	}
	if err := p.validator.ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p *Planner) Decide(ctx context.Context, state AgentState) (Decision, error) {
	decision, err := p.provider.Decide(ctx, state)
	if err == nil {
		err = p.validator.ValidateDecision(decision, state)
	}
	if err == nil {
		return decision, nil
	}
	if !errors.Is(err, ErrInvalidDecision) {
		return Decision{}, fmt.Errorf("generate decision: %w", err)
	}

	// Model-compatible JSON mode guarantees syntax, not business validity. Give
	// the model one bounded chance to correct a rejected decision with the
	// deterministic validator's reason; never loop indefinitely.
	repairState := state
	repairState.DecisionFeedback = err.Error()
	repaired, repairErr := p.provider.Decide(ctx, repairState)
	if repairErr != nil {
		return Decision{}, fmt.Errorf("repair invalid decision: %w", repairErr)
	}
	if repairErr := p.validator.ValidateDecision(repaired, state); repairErr != nil {
		return Decision{}, fmt.Errorf("repair invalid decision: %w", repairErr)
	}
	return repaired, nil
}
