package config

import (
	"os"
	"strings"
)

type Config struct {
	Database   DatabaseConfig
	Redis      RedisConfig
	RabbitMQ   RabbitMQConfig
	Qdrant     QdrantConfig
	AI         AIConfig
	Checkpoint CheckpointConfig
	Server     ServerConfig
}

type CheckpointConfig struct {
	Dir string
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type RedisConfig struct {
	Addr string
}

type RabbitMQConfig struct {
	URL string
}

type QdrantConfig struct {
	URL        string
	Collection string
	APIKey     string
}

type AIConfig struct {
	BaseURL          string
	APIKey           string
	ChatModel        string
	EmbeddingModel   string
	HTTPAllowedHosts []string
}

func Load() Config {
	return Config{
		Database: DatabaseConfig{
			Host:     envOrDefault("DB_HOST", "127.0.0.1"),
			Port:     envOrDefault("DB_PORT", "3306"),
			User:     envOrDefault("DB_USER", "minikvx"),
			Password: os.Getenv("DB_PASSWORD"),
			Name:     envOrDefault("DB_NAME", "minikvx_agent"),
		},
		Redis: RedisConfig{
			Addr: envOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		},
		RabbitMQ: RabbitMQConfig{
			URL: envOrDefault("RABBITMQ_URL", "amqp://guest:guest@127.0.0.1:5672/"),
		},
		Qdrant: QdrantConfig{
			URL:        envOrDefault("QDRANT_URL", "http://127.0.0.1:6334"),
			Collection: envOrDefault("QDRANT_COLLECTION", "flowpilot_knowledge"),
			APIKey:     os.Getenv("QDRANT_API_KEY"),
		},
		AI: AIConfig{
			BaseURL:          envOrDefault("AI_BASE_URL", "https://api.siliconflow.cn/v1"),
			APIKey:           os.Getenv("AI_API_KEY"),
			ChatModel:        os.Getenv("AI_CHAT_MODEL"),
			EmbeddingModel:   os.Getenv("AI_EMBEDDING_MODEL"),
			HTTPAllowedHosts: splitList(os.Getenv("HTTP_TOOL_ALLOWED_HOSTS")),
		},
		Checkpoint: CheckpointConfig{
			Dir: envOrDefault("CHECKPOINT_DIR", "./data/checkpoints"),
		},
		Server: ServerConfig{
			Port: envOrDefault("APP_PORT", "8080"),
		},
	}
}

func splitList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
