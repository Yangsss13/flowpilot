package taskqueue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/executionlock"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/repository"
	"github.com/Yangsss13/flowpilot/internal/service"
	"github.com/Yangsss13/flowpilot/internal/taskqueue"
	"github.com/Yangsss13/flowpilot/internal/workerpool"
)

func TestRabbitMQDuplicateMessagesExecuteTaskOnce(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run RabbitMQ integration tests")
	}

	cfg := config.Load()
	db, err := database.OpenMySQL(cfg.Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	redisClient, err := database.OpenRedis(cfg.Redis)
	if err != nil {
		t.Fatalf("open Redis: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })
	rabbitConnection, err := database.OpenRabbitMQ(cfg.RabbitMQ)
	if err != nil {
		t.Fatalf("open RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = rabbitConnection.Close() })
	publisher, err := taskqueue.NewRabbitPublisher(rabbitConnection)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	t.Cleanup(func() { _ = publisher.Close() })

	adminChannel, err := rabbitConnection.Channel()
	if err != nil {
		t.Fatalf("open RabbitMQ admin channel: %v", err)
	}
	t.Cleanup(func() { _ = adminChannel.Close() })
	if _, err := adminChannel.QueuePurge(taskqueue.QueueName, false); err != nil {
		t.Fatalf("purge task queue before test: %v", err)
	}
	t.Cleanup(func() { _, _ = adminChannel.QueuePurge(taskqueue.QueueName, false) })

	taskRepository := repository.NewGormTaskRepository(db)
	executionRepository := repository.NewGormExecutionRepository(db)
	taskService := service.NewTaskService(taskRepository)
	task, err := taskService.Create(context.Background(), service.CreateTaskInput{
		Name: "rabbitmq-integration-" + time.Now().Format("20060102150405.000000000"),
		Steps: []service.CreateTaskStepInput{
			{Name: "hold lock", ActionType: "sleep", ActionPayload: json.RawMessage(`{"duration_ms":100}`)},
		},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", task.ID).Delete(&domain.ExecutionLog{})
		db.Where("task_id = ?", task.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, task.ID)
	})
	t.Cleanup(func() {
		_ = redisClient.Del(context.Background(), fmt.Sprintf("flowpilot:task:lock:%d", task.ID)).Err()
	})

	stepExecutor := executor.NewStepExecutor()
	taskExecutor := executor.NewTaskExecutor(taskRepository, executionRepository, stepExecutor)
	dispatcher := executor.NewTaskDispatcher(taskRepository, taskExecutor, nil)
	locker, err := executionlock.NewRedisTaskLocker(redisClient, time.Minute)
	if err != nil {
		t.Fatalf("create task locker: %v", err)
	}
	lockedRunner := executionlock.NewLockedTaskRunner(locker, dispatcher)
	pool, err := workerpool.New(context.Background(), lockedRunner, 4, 20)
	if err != nil {
		t.Fatalf("create worker pool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pool.Stop(ctx); err != nil {
			t.Errorf("stop worker pool: %v", err)
		}
	})
	consumer, err := taskqueue.NewConsumer(rabbitConnection, publisher, pool, 3)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	if err := consumer.Start(context.Background(), 4); err != nil {
		t.Fatalf("start consumer: %v", err)
	}
	consumerStopped := false
	t.Cleanup(func() {
		if consumerStopped {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := consumer.Stop(ctx); err != nil {
			t.Errorf("stop consumer: %v", err)
		}
	})

	const messages = 10
	var publishers sync.WaitGroup
	publishErrors := make(chan error, messages)
	publishers.Add(messages)
	for i := 0; i < messages; i++ {
		go func() {
			defer publishers.Done()
			publishErrors <- publisher.Publish(context.Background(), task.ID)
		}()
	}
	publishers.Wait()
	close(publishErrors)
	for err := range publishErrors {
		if err != nil {
			t.Fatalf("publish duplicate task message: %v", err)
		}
	}

	finished := waitForTaskStatus(t, taskRepository, task.ID, domain.StatusSuccess)
	if len(finished.Steps) != 1 || finished.Steps[0].Status != domain.StatusSuccess {
		t.Fatalf("unexpected finished task: %#v", finished)
	}
	waitForQueueToDrain(t, adminChannel)
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 3*time.Second)
	if err := consumer.Stop(stopCtx); err != nil {
		cancelStop()
		t.Fatalf("stop consumer after execution: %v", err)
	}
	cancelStop()
	consumerStopped = true
	waitForQueueToDrain(t, adminChannel)
	logs, err := executionRepository.ListLogs(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("list execution logs: %v", err)
	}
	if len(logs) != 4 {
		t.Fatalf("execution log count = %d, want 4; logs=%#v", len(logs), logs)
	}
}

func waitForTaskStatus(t *testing.T, tasks repository.TaskRepository, taskID uint64, want domain.Status) *domain.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := tasks.GetByID(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if task.Status == want {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %d did not reach status %s", taskID, want)
	return nil
}

func waitForQueueToDrain(t *testing.T, channel *amqp.Channel) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		queue, err := channel.QueueInspect(taskqueue.QueueName)
		if err != nil {
			t.Fatalf("inspect task queue: %v", err)
		}
		if queue.Messages == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task queue did not drain")
}
