package domain

import "fmt"

// Status represents the lifecycle state shared by tasks and task steps.
type Status string

const (
	StatusPending Status = "Pending"
	StatusRunning Status = "Running"
	StatusSuccess Status = "Success"
	StatusFailed  Status = "Failed"
)

var allowedTransitions = map[Status]map[Status]struct{}{
	StatusPending: {
		StatusRunning: {},
	},
	StatusRunning: {
		StatusSuccess: {},
		StatusFailed:  {},
	},
	StatusFailed: {
		StatusRunning: {},
	},
	StatusSuccess: {},
}

// IsValid reports whether status is one of the states known by the domain.
func (s Status) IsValid() bool {
	_, ok := allowedTransitions[s]
	return ok
}

// CanTransitionTo reports whether moving from the current status to next is legal.
func (s Status) CanTransitionTo(next Status) bool {
	nextStates, ok := allowedTransitions[s]
	if !ok || !next.IsValid() {
		return false
	}

	_, ok = nextStates[next]
	return ok
}

// ValidateTransition returns an error that contains enough context for callers
// to turn an illegal transition into an API or execution error.
func ValidateTransition(current, next Status) error {
	if !current.IsValid() {
		return fmt.Errorf("unknown current status %q", current)
	}
	if !next.IsValid() {
		return fmt.Errorf("unknown next status %q", next)
	}
	if !current.CanTransitionTo(next) {
		return fmt.Errorf("status transition %s -> %s is not allowed", current, next)
	}

	return nil
}
