package config

import "testing"

func TestLoadAIConfig(t *testing.T) {
	t.Setenv("AI_BASE_URL", "https://example.com/v1")
	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_CHAT_MODEL", "test-model")

	config := Load()
	if config.AI.BaseURL != "https://example.com/v1" || config.AI.APIKey != "test-key" || config.AI.ChatModel != "test-model" {
		t.Fatalf("AI config = %#v", config.AI)
	}
}

func TestLoadUsesSiliconFlowBaseURLByDefault(t *testing.T) {
	t.Setenv("AI_BASE_URL", "")

	config := Load()
	if config.AI.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("AI base URL = %q", config.AI.BaseURL)
	}
}
