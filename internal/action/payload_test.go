package action

import (
	"encoding/json"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		actionType string
		payload    string
		wantErr    bool
	}{
		{name: "valid sleep", actionType: "sleep", payload: `{"duration_ms":1}`},
		{name: "zero sleep", actionType: "sleep", payload: `{"duration_ms":0}`, wantErr: true},
		{name: "too long sleep", actionType: "sleep", payload: `{"duration_ms":30001}`, wantErr: true},
		{name: "valid HTTP", actionType: "http_mock", payload: `{"status":500}`},
		{name: "invalid HTTP status", actionType: "http_mock", payload: `{"status":0}`, wantErr: true},
		{name: "valid shell", actionType: "shell_mock", payload: `{"exit_code":1}`},
		{name: "unknown action", actionType: "email_mock", payload: `{}`, wantErr: true},
		{name: "invalid JSON", actionType: "sleep", payload: `{`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.actionType, json.RawMessage(tt.payload))
			if tt.wantErr && err == nil {
				t.Fatal("Validate() returned nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() returned error: %v", err)
			}
		})
	}
}
