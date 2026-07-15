package config

import "testing"

func TestLoadAIConfig(t *testing.T) {
	t.Setenv("AI_BASE_URL", "https://example.com/v1")
	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_CHAT_MODEL", "test-model")
	t.Setenv("AI_EMBEDDING_MODEL", "embedding-model")

	config := Load()
	if config.AI.BaseURL != "https://example.com/v1" || config.AI.APIKey != "test-key" || config.AI.ChatModel != "test-model" || config.AI.EmbeddingModel != "embedding-model" {
		t.Fatalf("AI config = %#v", config.AI)
	}
}

func TestLoadQdrantConfig(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "knowledge")
	t.Setenv("QDRANT_API_KEY", "qdrant-key")

	config := Load()
	if config.Qdrant.URL != "http://qdrant:6333" || config.Qdrant.Collection != "knowledge" || config.Qdrant.APIKey != "qdrant-key" {
		t.Fatalf("Qdrant config = %#v", config.Qdrant)
	}
}

func TestLoadUsesSiliconFlowBaseURLByDefault(t *testing.T) {
	t.Setenv("AI_BASE_URL", "")

	config := Load()
	if config.AI.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("AI base URL = %q", config.AI.BaseURL)
	}
}
