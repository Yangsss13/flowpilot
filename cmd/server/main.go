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

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/executionlock"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/handler"
	"github.com/Yangsss13/flowpilot/internal/httpapi"
	"github.com/Yangsss13/flowpilot/internal/repository"
	"github.com/Yangsss13/flowpilot/internal/service"
	"github.com/Yangsss13/flowpilot/internal/taskqueue"
	"github.com/Yangsss13/flowpilot/internal/workerpool"
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
	rabbitConnection, err := database.OpenRabbitMQ(cfg.RabbitMQ)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}
	defer rabbitConnection.Close()
	taskPublisher, err := taskqueue.NewRabbitPublisher(rabbitConnection)
	if err != nil {
		log.Fatalf("start RabbitMQ publisher: %v", err)
	}
	defer taskPublisher.Close()

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
	consumer, err := taskqueue.NewConsumer(rabbitConnection, taskPublisher, pool, 3)
	if err != nil {
		log.Fatalf("create RabbitMQ consumer: %v", err)
	}
	if err := consumer.Start(context.Background(), 4); err != nil {
		log.Fatalf("start RabbitMQ consumer: %v", err)
	}
	executionService := service.NewExecutionService(taskRepository, executionRepository, taskPublisher)
	taskHandler := handler.NewTaskHandler(taskService)
	executionHandler := handler.NewExecutionHandler(executionService)
	router := httpapi.NewRouter(taskHandler, executionHandler)

	address := ":" + cfg.Server.Port
	log.Printf("FlowPilot listening on %s", address)
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

	httpShutdownCtx, cancelHTTPShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := server.Shutdown(httpShutdownCtx); err != nil {
		log.Printf("shutdown HTTP server: %v", err)
	}
	cancelHTTPShutdown()
	consumerShutdownCtx, cancelConsumerShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := consumer.Stop(consumerShutdownCtx); err != nil {
		log.Printf("shutdown RabbitMQ consumer: %v", err)
	}
	cancelConsumerShutdown()
	poolShutdownCtx, cancelPoolShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := pool.Stop(poolShutdownCtx); err != nil {
		log.Printf("shutdown worker pool: %v", err)
	}
	cancelPoolShutdown()
}
