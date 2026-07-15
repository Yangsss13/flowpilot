package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

const MaxAgentIterations = 20

var ErrAgentExecution = errors.New("agent execution failed")

type AgentPlanner interface {
	Decide(ctx context.Context, state agent.AgentState) (agent.Decision, error)
	Replan(ctx context.Context, state agent.AgentState, reason string) (agent.Plan, error)
}

type AgentToolRunner interface {
	Execute(ctx context.Context, tool agent.ToolName, input json.RawMessage) (json.RawMessage, error)
}

type AgentStateStore interface {
	TransitionTask(ctx context.Context, taskID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	TransitionStep(ctx context.Context, taskID, stepID uint64, current, next domain.Status, level domain.LogLevel, message string) error
	CompleteAgentStep(ctx context.Context, taskID, stepID uint64, next domain.Status, observation agent.Observation) error
	CompleteAgentTask(ctx context.Context, taskID uint64, next domain.Status, result, message string) error
	ReplaceAgentPlan(ctx context.Context, taskID uint64, plan agent.Plan, replanCount int) ([]domain.TaskStep, error)
}

type AgentRunner struct {
	tasks   TaskSource
	states  AgentStateStore
	planner AgentPlanner
	tools   AgentToolRunner
}

func NewAgentRunner(tasks TaskSource, states AgentStateStore, planner AgentPlanner, tools AgentToolRunner) *AgentRunner {
	return &AgentRunner{tasks: tasks, states: states, planner: planner, tools: tools}
}

func (r *AgentRunner) Execute(ctx context.Context, taskID uint64) error {
	task, err := r.tasks.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task %d: %w", taskID, err)
	}
	if task.TaskType != domain.TaskTypeAgent || (task.Status != domain.StatusPending && task.Status != domain.StatusFailed) {
		return fmt.Errorf("%w: task %d has type %s and status %s", ErrTaskNotRunnable, task.ID, task.TaskType, task.Status)
	}
	state, err := agentStateFromTask(task)
	if err != nil {
		return fmt.Errorf("load agent state: %w", err)
	}
	if err := r.states.TransitionTask(ctx, task.ID, task.Status, domain.StatusRunning, domain.LogLevelInfo, "agent task started"); err != nil {
		return fmt.Errorf("start agent task %d: %w", task.ID, err)
	}
	task.Status = domain.StatusRunning

	for iteration := 0; iteration < MaxAgentIterations; iteration++ {
		decision, err := r.planner.Decide(ctx, state)
		if err != nil {
			r.failTask(task.ID, "agent decision failed")
			return fmt.Errorf("decide agent task %d: %w", task.ID, err)
		}
		switch decision.Action {
		case agent.DecisionFinish:
			if err := r.completeTask(task.ID, domain.StatusSuccess, decision.FinalAnswer, "agent task succeeded"); err != nil {
				r.failTask(task.ID, "persist agent success failed")
				return err
			}
			return nil
		case agent.DecisionFail:
			if err := r.completeTask(task.ID, domain.StatusFailed, decision.Reason, "agent task failed: "+decision.Reason); err != nil {
				r.failTask(task.ID, "persist agent failure failed")
				return err
			}
			return fmt.Errorf("%w: %s", ErrAgentExecution, decision.Reason)
		case agent.DecisionReplan:
			plan, err := r.planner.Replan(ctx, state, decision.Reason)
			if err != nil {
				r.failTask(task.ID, "agent replan failed")
				return fmt.Errorf("replan agent task %d: %w", task.ID, err)
			}
			state.ReplanCount++
			steps, err := r.states.ReplaceAgentPlan(ctx, task.ID, plan, state.ReplanCount)
			if err != nil {
				r.failTask(task.ID, "persist replacement plan failed")
				return fmt.Errorf("persist replacement plan: %w", err)
			}
			task.Steps = steps
			state.Plan = plan
		case agent.DecisionContinue:
			step, err := runnableAgentStep(task.Steps, decision.NextStepID)
			if err != nil {
				_ = r.completeTask(task.ID, domain.StatusFailed, err.Error(), "agent task failed: invalid next step")
				return fmt.Errorf("%w: %v", ErrAgentExecution, err)
			}
			if err := r.states.TransitionStep(ctx, task.ID, step.ID, step.Status, domain.StatusRunning, domain.LogLevelInfo, "agent step started"); err != nil {
				r.failTask(task.ID, "start agent step failed")
				return fmt.Errorf("start agent step %d: %w", step.ID, err)
			}
			step.Status = domain.StatusRunning
			output, toolErr := r.tools.Execute(ctx, agent.ToolName(step.ActionType), step.ActionPayload)
			observation := agent.Observation{StepID: step.Name, Output: output}
			next := domain.StatusSuccess
			if toolErr != nil {
				next = domain.StatusFailed
				observation.Error = toolErr.Error()
			}
			if err := r.completeStep(task.ID, step.ID, next, observation); err != nil {
				r.failTask(task.ID, "persist agent observation failed")
				return err
			}
			step.Status = next
			step.Observation, _ = json.Marshal(observation)
			state.Observations = append(state.Observations, observation)
		}
	}
	reason := fmt.Sprintf("agent exceeded %d decision iterations", MaxAgentIterations)
	if err := r.completeTask(task.ID, domain.StatusFailed, reason, "agent task failed: iteration limit"); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrAgentExecution, reason)
}

func agentStateFromTask(task *domain.Task) (agent.AgentState, error) {
	state := agent.AgentState{Goal: task.Description, ReplanCount: task.ReplanCount}
	state.Plan.Steps = make([]agent.PlanStep, len(task.Steps))
	for index := range task.Steps {
		step := task.Steps[index]
		var dependencies []string
		if len(step.DependsOn) > 0 {
			if err := json.Unmarshal(step.DependsOn, &dependencies); err != nil {
				return agent.AgentState{}, fmt.Errorf("decode dependencies for step %d: %w", step.ID, err)
			}
		}
		state.Plan.Steps[index] = agent.PlanStep{ID: step.Name, Tool: agent.ToolName(step.ActionType), Input: step.ActionPayload, DependsOn: dependencies}
		if len(step.Observation) > 0 {
			var observation agent.Observation
			if err := json.Unmarshal(step.Observation, &observation); err != nil {
				return agent.AgentState{}, fmt.Errorf("decode observation for step %d: %w", step.ID, err)
			}
			state.Observations = append(state.Observations, observation)
		}
	}
	return state, nil
}

func runnableAgentStep(steps []domain.TaskStep, id string) (*domain.TaskStep, error) {
	statusByName := make(map[string]domain.Status, len(steps))
	var target *domain.TaskStep
	for index := range steps {
		statusByName[steps[index].Name] = steps[index].Status
		if steps[index].Name == id {
			target = &steps[index]
		}
	}
	if target == nil {
		return nil, fmt.Errorf("step %q does not exist", id)
	}
	if target.Status == domain.StatusSuccess || target.Status == domain.StatusRunning {
		return nil, fmt.Errorf("step %q has status %s", id, target.Status)
	}
	var dependencies []string
	if len(target.DependsOn) > 0 {
		if err := json.Unmarshal(target.DependsOn, &dependencies); err != nil {
			return nil, fmt.Errorf("decode dependencies for step %q: %w", id, err)
		}
	}
	for _, dependency := range dependencies {
		if statusByName[dependency] != domain.StatusSuccess {
			return nil, fmt.Errorf("step %q dependency %q is not successful", id, dependency)
		}
	}
	return target, nil
}

func (r *AgentRunner) completeStep(taskID, stepID uint64, next domain.Status, observation agent.Observation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return r.states.CompleteAgentStep(ctx, taskID, stepID, next, observation)
}

func (r *AgentRunner) completeTask(taskID uint64, next domain.Status, result, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return r.states.CompleteAgentTask(ctx, taskID, next, result, message)
}

func (r *AgentRunner) failTask(taskID uint64, message string) {
	_ = r.completeTask(taskID, domain.StatusFailed, message, message)
}
