package main

import (
	"log"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
)

func main() {
	cfg := config.Load()

	db, err := database.OpenMySQL(cfg.Database)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("start server: %v", err)
	}

	log.Println("MiniKVX-Agent database is ready")
}
