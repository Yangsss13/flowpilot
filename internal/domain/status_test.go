package domain

import "testing"

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name    string
		current Status
		next    Status
		wantErr bool
	}{
		{name: "start pending task", current: StatusPending, next: StatusRunning},
		{name: "finish successfully", current: StatusRunning, next: StatusSuccess},
		{name: "finish with failure", current: StatusRunning, next: StatusFailed},
		{name: "retry failed task", current: StatusFailed, next: StatusRunning},
		{name: "cannot rerun successful task", current: StatusSuccess, next: StatusRunning, wantErr: true},
		{name: "cannot skip running", current: StatusPending, next: StatusSuccess, wantErr: true},
		{name: "cannot remain in same state", current: StatusRunning, next: StatusRunning, wantErr: true},
		{name: "reject unknown current state", current: Status("Unknown"), next: StatusRunning, wantErr: true},
		{name: "reject unknown next state", current: StatusPending, next: Status("Unknown"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.current, tt.next)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateTransition(%q, %q) returned nil, want error", tt.current, tt.next)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTransition(%q, %q) returned error: %v", tt.current, tt.next, err)
			}
		})
	}
}
