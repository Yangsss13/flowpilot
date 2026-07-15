package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yangsss13/flowpilot/internal/executionlock"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

type TaskRunner interface {
	Execute(ctx context.Context, taskID uint64) error
}

type RetryPublisher interface {
	PublishRetry(ctx context.Context, taskID uint64, retryCount int) error
}

type Consumer struct {
	connection *amqp.Connection
	publisher  RetryPublisher
	runner     TaskRunner
	maxRetries int

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	channels []*amqp.Channel
}

func NewConsumer(connection *amqp.Connection, publisher RetryPublisher, runner TaskRunner, maxRetries int) (*Consumer, error) {
	if maxRetries < 0 {
		return nil, fmt.Errorf("max retries must not be negative")
	}
	return &Consumer{
		connection: connection,
		publisher:  publisher,
		runner:     runner,
		maxRetries: maxRetries,
	}, nil
}

func (c *Consumer) Start(parent context.Context, workerCount int) error {
	if workerCount <= 0 {
		return fmt.Errorf("consumer worker count must be positive")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return fmt.Errorf("consumer already started")
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})

	var workers sync.WaitGroup
	for workerID := 1; workerID <= workerCount; workerID++ {
		channel, deliveries, err := c.openWorker(ctx, workerID)
		if err != nil {
			cancel()
			for _, opened := range c.channels {
				_ = opened.Close()
			}
			workers.Wait()
			c.cancel = nil
			c.channels = nil
			return err
		}
		c.channels = append(c.channels, channel)
		workers.Add(1)
		go func(id int, messages <-chan amqp.Delivery) {
			defer workers.Done()
			c.consume(ctx, id, messages)
		}(workerID, deliveries)
	}
	go func() {
		workers.Wait()
		close(c.done)
	}()
	return nil
}

func (c *Consumer) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel == nil {
		c.mu.Unlock()
		return nil
	}
	c.cancel()
	done := c.done
	channels := append([]*amqp.Channel(nil), c.channels...)
	c.mu.Unlock()

	select {
	case <-done:
		for _, channel := range channels {
			_ = channel.Close()
		}
		return nil
	case <-ctx.Done():
		for _, channel := range channels {
			_ = channel.Close()
		}
		return fmt.Errorf("stop RabbitMQ consumer: %w", ctx.Err())
	}
}

func (c *Consumer) openWorker(ctx context.Context, workerID int) (*amqp.Channel, <-chan amqp.Delivery, error) {
	channel, err := c.connection.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("open consumer %d channel: %w", workerID, err)
	}
	if _, err := declareQueue(channel); err != nil {
		_ = channel.Close()
		return nil, nil, err
	}
	if err := channel.Qos(1, 0, false); err != nil {
		_ = channel.Close()
		return nil, nil, fmt.Errorf("set consumer %d QoS: %w", workerID, err)
	}
	deliveries, err := channel.ConsumeWithContext(
		ctx,
		QueueName,
		fmt.Sprintf("flowpilot-worker-%d", workerID),
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = channel.Close()
		return nil, nil, fmt.Errorf("start consumer %d: %w", workerID, err)
	}
	return channel, deliveries, nil
}

func (c *Consumer) consume(ctx context.Context, workerID int, deliveries <-chan amqp.Delivery) {
	for delivery := range deliveries {
		if err := c.handleDelivery(ctx, delivery); err != nil {
			log.Printf("RabbitMQ consumer %d handle delivery: %v", workerID, err)
		}
	}
}

func (c *Consumer) handleDelivery(ctx context.Context, delivery amqp.Delivery) error {
	message, err := decodeTaskMessage(delivery.Body)
	if err != nil {
		log.Printf("discard invalid RabbitMQ task message: %v", err)
		return delivery.Ack(false)
	}
	retryCount, err := messageRetryCount(delivery.Headers)
	if err != nil {
		log.Printf("discard task %d with invalid retry header: %v", message.TaskID, err)
		return delivery.Ack(false)
	}

	executionErr := c.runner.Execute(ctx, message.TaskID)
	if errors.Is(executionErr, executionlock.ErrNotAcquired) && delivery.Redelivered {
		// A broker redelivery can arrive before the crashed process's Redis lock
		// expires. Keep the message unconfirmed and retry later; newly published
		// duplicates still take the fast ACK path below.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		return delivery.Nack(false, true)
	}
	if executionErr == nil || isFinalExecutionError(executionErr) {
		return delivery.Ack(false)
	}
	if errors.Is(executionErr, context.Canceled) || errors.Is(executionErr, context.DeadlineExceeded) {
		return delivery.Nack(false, true)
	}
	if retryCount >= c.maxRetries {
		log.Printf("task %d exhausted %d RabbitMQ retries: %v", message.TaskID, retryCount, executionErr)
		return delivery.Ack(false)
	}

	if err := c.publisher.PublishRetry(ctx, message.TaskID, retryCount+1); err != nil {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return errors.Join(fmt.Errorf("publish task %d retry: %w", message.TaskID, err), nackErr)
		}
		return fmt.Errorf("publish task %d retry: %w", message.TaskID, err)
	}
	return delivery.Ack(false)
}

func decodeTaskMessage(body []byte) (TaskMessage, error) {
	var message TaskMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return TaskMessage{}, fmt.Errorf("decode task message: %w", err)
	}
	if message.TaskID == 0 {
		return TaskMessage{}, fmt.Errorf("task_id must be positive")
	}
	return message, nil
}

func messageRetryCount(headers amqp.Table) (int, error) {
	value, ok := headers[retryHeader]
	if !ok {
		return 0, nil
	}
	var retryCount int64
	switch typed := value.(type) {
	case int32:
		retryCount = int64(typed)
	case int64:
		retryCount = typed
	case int:
		retryCount = int64(typed)
	default:
		return 0, fmt.Errorf("%s has type %T", retryHeader, value)
	}
	if retryCount < 0 || retryCount > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("%s is out of range", retryHeader)
	}
	return int(retryCount), nil
}

func isFinalExecutionError(err error) bool {
	return errors.Is(err, executionlock.ErrNotAcquired) ||
		errors.Is(err, executor.ErrTaskNotRunnable) ||
		errors.Is(err, executor.ErrAgentExecution) ||
		errors.Is(err, executor.ErrStepExecution) ||
		errors.Is(err, repository.ErrStateConflict)
}
