package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
	"minikvx-agent/internal/executionlock"
	"minikvx-agent/internal/executor"
	"minikvx-agent/internal/handler"
	"minikvx-agent/internal/httpapi"
	"minikvx-agent/internal/repository"
	"minikvx-agent/internal/service"
	"minikvx-agent/internal/workerpool"
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
	redisClient, err := database.OpenRedis(cfg.Redis)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}
	defer redisClient.Close()

	taskRepository := repository.NewGormTaskRepository(db)
	executionRepository := repository.NewGormExecutionRepository(db)
	taskService := service.NewTaskService(taskRepository)
	stepExecutor := executor.NewStepExecutor()
	taskExecutor := executor.NewTaskExecutor(taskRepository, executionRepository, stepExecutor)
	taskLock, err := executionlock.NewRedisTaskLocker(redisClient, 5*time.Minute)
	if err != nil {
		log.Fatalf("start task execution lock: %v", err)
	}
	lockedTaskExecutor := executionlock.NewLockedTaskRunner(taskLock, taskExecutor)
	appCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	pool, err := workerpool.New(context.Background(), lockedTaskExecutor, 4, 100)
	if err != nil {
		log.Fatalf("start worker pool: %v", err)
	}
	executionService := service.NewExecutionService(taskRepository, executionRepository, pool)
	taskHandler := handler.NewTaskHandler(taskService)
	executionHandler := handler.NewExecutionHandler(executionService)
	router := httpapi.NewRouter(taskHandler, executionHandler)

	address := ":" + cfg.Server.Port
	log.Printf("MiniKVX-Agent listening on %s", address)
	server := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-appCtx.Done():
		log.Println("shutdown signal received")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped unexpectedly: %v", err)
		}
		stopSignals()
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown HTTP server: %v", err)
	}
	if err := pool.Stop(shutdownCtx); err != nil {
		log.Printf("shutdown worker pool: %v", err)
	}
}
