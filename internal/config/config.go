package config

import "os"

type Config struct {
	Database DatabaseConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	AI       AIConfig
	Server   ServerConfig
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

type AIConfig struct {
	BaseURL   string
	APIKey    string
	ChatModel string
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
		AI: AIConfig{
			BaseURL:   envOrDefault("AI_BASE_URL", "https://api.siliconflow.cn/v1"),
			APIKey:    os.Getenv("AI_API_KEY"),
			ChatModel: os.Getenv("AI_CHAT_MODEL"),
		},
		Server: ServerConfig{
			Port: envOrDefault("APP_PORT", "8080"),
		},
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
