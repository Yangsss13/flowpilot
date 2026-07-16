package config

import (
	"testing"
	"time"
)

func TestLoadAIConfig(t *testing.T) {
	t.Setenv("AI_BASE_URL", "https://example.com/v1")
	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_CHAT_MODEL", "test-model")
	t.Setenv("AI_EMBEDDING_MODEL", "embedding-model")
	t.Setenv("HTTP_TOOL_ALLOWED_HOSTS", "api.example.com, data.example.com")

	config := Load()
	if config.AI.BaseURL != "https://example.com/v1" || config.AI.APIKey != "test-key" || config.AI.ChatModel != "test-model" || config.AI.EmbeddingModel != "embedding-model" {
		t.Fatalf("AI config = %#v", config.AI)
	}
	if len(config.AI.HTTPAllowedHosts) != 2 || config.AI.HTTPAllowedHosts[0] != "api.example.com" {
		t.Fatalf("allowed HTTP hosts = %v", config.AI.HTTPAllowedHosts)
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

func TestLoadUsesLocalCheckpointDirByDefault(t *testing.T) {
	t.Setenv("CHECKPOINT_DIR", "")

	config := Load()
	if config.Checkpoint.Dir != "./data/checkpoints" {
		t.Fatalf("checkpoint dir = %q", config.Checkpoint.Dir)
	}
}

func TestLoadKnowledgeLimitsAreConfigurable(t *testing.T) {
	t.Setenv("KNOWLEDGE_MAX_PDF_BYTES", "12345")
	t.Setenv("KNOWLEDGE_MAX_PPT_SLIDES", "42")
	t.Setenv("KNOWLEDGE_PARSE_TIMEOUT", "12s")
	t.Setenv("KNOWLEDGE_SEARCH_MIN_SCORE", "0.65")
	config := Load()
	if config.Knowledge.MaxBytesByFormat[".pdf"] != 12345 || config.Knowledge.MaxPPTSlides != 42 ||
		config.Knowledge.ParseTimeout.String() != "12s" || config.Knowledge.SearchMinScore != 0.65 {
		t.Fatalf("Knowledge config = %#v", config.Knowledge)
	}
}

func TestLoadMediaIngestionConfig(t *testing.T) {
	t.Setenv("KNOWLEDGE_MAX_MP4_BYTES", "123456")
	t.Setenv("KNOWLEDGE_MAX_MEDIA_DURATION", "30m")
	t.Setenv("KNOWLEDGE_MEDIA_CONCURRENCY", "2")
	t.Setenv("WHISPER_MODEL_PATH", "model.bin")
	t.Setenv("OCR_LANGUAGES", "chi_sim+eng")
	config := Load()
	if config.Knowledge.MaxBytesByFormat[".mp4"] != 123456 || config.Knowledge.MaxMediaDuration != 30*time.Minute ||
		config.Knowledge.MediaConcurrency != 2 || config.Knowledge.WhisperModelPath != "model.bin" || config.Knowledge.OCRLanguages != "chi_sim+eng" {
		t.Fatalf("media config = %#v", config.Knowledge)
	}
}
