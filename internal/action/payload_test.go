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
		{name: "sleep unknown field", actionType: "sleep", payload: `{"duration_ms":1,"extra":true}`, wantErr: true},
		{name: "zero sleep", actionType: "sleep", payload: `{"duration_ms":0}`, wantErr: true},
		{name: "too long sleep", actionType: "sleep", payload: `{"duration_ms":30001}`, wantErr: true},
		{name: "valid HTTP", actionType: "http_mock", payload: `{"status":500}`},
		{name: "invalid HTTP status", actionType: "http_mock", payload: `{"status":0}`, wantErr: true},
		{name: "valid shell", actionType: "shell_mock", payload: `{"exit_code":1}`},
		{name: "negative shell exit", actionType: "shell_mock", payload: `{"exit_code":-1}`, wantErr: true},
		{name: "large shell exit", actionType: "shell_mock", payload: `{"exit_code":256}`, wantErr: true},
		{name: "RAG disabled", actionType: "rag_query", payload: `{"query":"policy"}`, wantErr: true},
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

func TestValidateRAGQueryWithCapability(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "valid defaults", payload: `{"query":" policy "}`},
		{name: "valid options", payload: `{"query":"policy","top_k":5,"min_score":0.6}`},
		{name: "empty query", payload: `{"query":" "}`, wantErr: true},
		{name: "top k too large", payload: `{"query":"policy","top_k":11}`, wantErr: true},
		{name: "negative score", payload: `{"query":"policy","min_score":-0.1}`, wantErr: true},
		{name: "unknown field", payload: `{"query":"policy","extra":true}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate("rag_query", json.RawMessage(tt.payload), Capabilities{RAGQuery: true})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateLLMSummarizeWithCapability(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "valid", payload: `{"instruction":"生成带引用的总结"}`},
		{name: "empty instruction", payload: `{"instruction":" "}`, wantErr: true},
		{name: "unknown field", payload: `{"instruction":"总结","prompt":"ignored"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate("llm_summarize", json.RawMessage(tt.payload), Capabilities{LLMSummarize: true})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	if err := Validate("llm_summarize", json.RawMessage(`{"instruction":"总结"}`)); err == nil {
		t.Fatal("Validate() accepted llm_summarize without capability")
	}
}
