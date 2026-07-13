package main

import (
	"log"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
	"minikvx-agent/internal/handler"
	"minikvx-agent/internal/httpapi"
	"minikvx-agent/internal/repository"
	"minikvx-agent/internal/service"
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

	taskRepository := repository.NewGormTaskRepository(db)
	taskService := service.NewTaskService(taskRepository)
	taskHandler := handler.NewTaskHandler(taskService)
	router := httpapi.NewRouter(taskHandler)

	address := ":" + cfg.Server.Port
	log.Printf("MiniKVX-Agent listening on %s", address)
	if err := router.Run(address); err != nil {
		log.Fatalf("run HTTP server: %v", err)
	}
}
