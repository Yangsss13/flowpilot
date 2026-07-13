package config

import "os"

type Config struct {
	Database DatabaseConfig
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
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
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
