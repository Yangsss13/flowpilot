package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yangsss13/flowpilot/internal/agent"
	"github.com/Yangsss13/flowpilot/internal/checkpoint"
	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/executionlock"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/handler"
	"github.com/Yangsss13/flowpilot/internal/httpapi"
	"github.com/Yangsss13/flowpilot/internal/knowledge"
	"github.com/Yangsss13/flowpilot/internal/rag"
	"github.com/Yangsss13/flowpilot/internal/repository"
	"github.com/Yangsss13/flowpilot/internal/service"
	"github.com/Yangsss13/flowpilot/internal/taskqueue"
	"github.com/Yangsss13/flowpilot/internal/workerpool"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "knowledge-parse" {
		if err := knowledge.RunParserCommand(os.Stdin, os.Stdout); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "knowledge parser failed")
			os.Exit(1)
		}
		return
	}
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
	qdrantStore, err := rag.NewQdrantStore(cfg.Qdrant.URL, cfg.Qdrant.Collection, cfg.Qdrant.APIKey, nil)
	if err != nil {
		log.Fatalf("configure Qdrant: %v", err)
	}
	var ragService *rag.Service
	var knowledgeHandler *handler.KnowledgeHandler
	var knowledgePublisher *knowledge.RabbitPublisher
	var knowledgeConsumer *knowledge.Consumer
	var knowledgeDispatcher *knowledge.Dispatcher
	var knowledgeToolSearcher agent.RAGSearcher
	mediaEnabled := false
	if cfg.AI.EmbeddingModel == "" {
		log.Println("Knowledge API disabled: set AI_API_KEY and AI_EMBEDDING_MODEL to enable it")
	} else {
		if cfg.AI.APIKey == "" {
			log.Fatal("start Knowledge API: AI_API_KEY is required when AI_EMBEDDING_MODEL is set")
		}
		embedder, err := rag.NewOpenAICompatibleEmbedder(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.EmbeddingModel, nil)
		if err != nil {
			log.Fatalf("start Knowledge API embedder: %v", err)
		}
		ragService = rag.NewService(embedder, qdrantStore)
		knowledgeStorage, err := knowledge.NewLocalObjectStorage(cfg.Knowledge.StorageDir)
		if err != nil {
			log.Fatalf("start Knowledge object storage: %v", err)
		}
		knowledgeRepository := knowledge.NewGormRepository(db)
		knowledgePublisher, err = knowledge.NewRabbitPublisher(rabbitConnection)
		if err != nil {
			log.Fatalf("start Knowledge publisher: %v", err)
		}
		defer knowledgePublisher.Close()
		knowledgeParser, err := knowledge.NewSubprocessParser()
		if err != nil {
			log.Fatalf("start Knowledge parser: %v", err)
		}
		var mediaPipeline *knowledge.MediaPipeline
		if err := knowledge.CheckMediaRuntime(cfg.Knowledge); err != nil {
			log.Printf("Media ingestion disabled: %v", err)
		} else {
			mediaEnabled = true
			mediaProcessor := knowledge.NewFFmpegProcessor(cfg.Knowledge, nil)
			transcriber := knowledge.NewWhisperCPPTranscriber(cfg.Knowledge, nil)
			ocr := knowledge.NewTesseractOCR(cfg.Knowledge, nil)
			mediaPipeline = knowledge.NewMediaPipeline(knowledgeStorage, knowledgeRepository, mediaProcessor, transcriber, ocr, cfg.Knowledge)
		}
		knowledgeWorker := knowledge.NewWorker(knowledgeRepository, knowledgeStorage, knowledgeParser, ragService, cfg.Knowledge, mediaPipeline)
		knowledgeConsumer = knowledge.NewConsumer(rabbitConnection, knowledgeWorker)
		if err := knowledgeConsumer.Start(context.Background(), cfg.Knowledge.WorkerCount); err != nil {
			log.Fatalf("start Knowledge consumer: %v", err)
		}
		knowledgeDispatcher = knowledge.NewDispatcher(knowledgeRepository, knowledgePublisher, knowledgeWorker, cfg.Knowledge.DispatchInterval)
		if err := knowledgeDispatcher.Start(context.Background()); err != nil {
			log.Fatalf("start Knowledge dispatcher: %v", err)
		}
		knowledgeService := knowledge.NewService(knowledgeRepository, knowledgeStorage, knowledgePublisher, ragService, cfg.Knowledge, mediaEnabled)
		knowledgeToolSearcher = knowledge.NewAgentSearcher(knowledgeService)
		var maxUploadBytes int64
		for _, maximum := range cfg.Knowledge.MaxBytesByFormat {
			if maximum > maxUploadBytes {
				maxUploadBytes = maximum
			}
		}
		knowledgeHandler = handler.NewKnowledgeHandler(knowledgeService, maxUploadBytes)
	}
	toolRegistry, err := agent.NewToolRegistry(knowledgeToolSearcher, cfg.AI.HTTPAllowedHosts, nil)
	if err != nil {
		log.Fatalf("configure Agent tools: %v", err)
	}
	toolDefinitions := toolRegistry.Definitions()
	var planner *agent.Planner
	if cfg.AI.ChatModel == "" {
		log.Println("Agent API disabled: set AI_API_KEY and AI_CHAT_MODEL to enable it")
	} else if len(toolDefinitions) == 0 {
		log.Println("Agent API disabled: configure RAG or at least one HTTP_TOOL_ALLOWED_HOSTS entry")
	} else {
		if cfg.AI.APIKey == "" {
			log.Fatal("start Agent API: AI_API_KEY is required when AI_CHAT_MODEL is set")
		}
		provider, err := agent.NewOpenAICompatibleProvider(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.ChatModel, nil)
		if err != nil {
			log.Fatalf("start Agent API: %v", err)
		}
		validator, err := agent.NewValidator(toolDefinitions, agent.MaxPlanSteps)
		if err != nil {
			log.Fatalf("start Agent API validator: %v", err)
		}
		planner = agent.NewPlanner(provider, toolDefinitions, validator)
	}
	taskService := service.NewTaskService(taskRepository, ragService != nil)
	var workflowSearcher executor.WorkflowSearcher
	if ragService != nil {
		workflowSearcher = ragService
	}
	stepExecutor := executor.NewStepExecutor(workflowSearcher)
	taskExecutor := executor.NewTaskExecutor(taskRepository, executionRepository, stepExecutor)
	var agentRunner executor.TaskRunner
	var agentHandler *handler.AgentHandler
	if planner != nil {
		checkpointStore, err := checkpoint.Open(cfg.Checkpoint.Dir)
		if err != nil {
			log.Fatalf("start Agent checkpoint store: %v", err)
		}
		defer func() {
			if err := checkpointStore.Close(); err != nil {
				log.Printf("close Agent checkpoint store: %v", err)
			}
		}()
		agentRunner = executor.NewAgentRunner(taskRepository, executionRepository, planner, toolRegistry, checkpointStore)
		agentHandler = handler.NewAgentHandler(
			service.NewAgentService(planner, taskRepository),
			service.NewAgentExecutionService(taskRepository, taskPublisher),
		)
	}
	dispatcher := executor.NewTaskDispatcher(taskRepository, taskExecutor, agentRunner)
	taskLock, err := executionlock.NewRedisTaskLocker(redisClient, 5*time.Minute)
	if err != nil {
		log.Fatalf("start task execution lock: %v", err)
	}
	lockedTaskExecutor := executionlock.NewLockedTaskRunner(taskLock, dispatcher)
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
	knowledgeFormats := []string{".txt", ".md", ".pdf", ".docx", ".pptx"}
	if mediaEnabled {
		knowledgeFormats = append(knowledgeFormats, ".mp3", ".wav", ".m4a", ".mp4", ".mov", ".webm")
	}
	knowledgeLimits := make(map[string]int64, len(knowledgeFormats))
	for _, format := range knowledgeFormats {
		knowledgeLimits[format] = cfg.Knowledge.MaxBytesByFormat[format]
	}
	knowledgeCapability := handler.KnowledgeCapability{
		AsyncIngestion: true, MediaIngestion: mediaEnabled,
		SupportedFormats: knowledgeFormats, MaxBytesByFormat: knowledgeLimits,
		MaxMediaDurationSeconds: int64(cfg.Knowledge.MaxMediaDuration.Seconds()),
	}
	capabilityHandler := handler.NewCapabilityHandler(planner != nil, toolDefinitions, knowledgeHandler != nil, knowledgeCapability)
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get MySQL pool for readiness: %v", err)
	}
	readinessChecks := map[string]handler.ReadinessCheck{
		"mysql": func(ctx context.Context) error { return sqlDB.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return redisClient.Ping(ctx).Err() },
		"rabbitmq": func(context.Context) error {
			if rabbitConnection.IsClosed() {
				return errors.New("RabbitMQ connection is closed")
			}
			return nil
		},
		"qdrant": qdrantStore.Health,
	}
	if cfg.AI.ChatModel != "" {
		readinessChecks["agent"] = func(context.Context) error {
			if planner == nil {
				return errors.New("Agent is not configured with an executable tool")
			}
			return nil
		}
	}
	if cfg.AI.EmbeddingModel != "" {
		readinessChecks["knowledge"] = func(context.Context) error {
			if ragService == nil {
				return errors.New("knowledge service is not configured")
			}
			return nil
		}
	}
	if mediaEnabled {
		readinessChecks["media"] = func(context.Context) error { return knowledge.CheckMediaRuntime(cfg.Knowledge) }
	}
	healthHandler := handler.NewHealthHandler(readinessChecks)
	router := httpapi.NewRouter(taskHandler, executionHandler, agentHandler, knowledgeHandler, capabilityHandler, healthHandler)

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
	if knowledgeDispatcher != nil {
		dispatcherShutdownCtx, cancelDispatcherShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		if err := knowledgeDispatcher.Stop(dispatcherShutdownCtx); err != nil {
			log.Printf("shutdown Knowledge dispatcher: %v", err)
		}
		cancelDispatcherShutdown()
	}
	if knowledgeConsumer != nil {
		knowledgeShutdownCtx, cancelKnowledgeShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		if err := knowledgeConsumer.Stop(knowledgeShutdownCtx); err != nil {
			log.Printf("shutdown Knowledge consumer: %v", err)
		}
		cancelKnowledgeShutdown()
	}
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
