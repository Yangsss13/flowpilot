package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const QueueName = "flowpilot.tasks"
const retryHeader = "x-retry-count"

type TaskMessage struct {
	TaskID uint64 `json:"task_id"`
}

type RabbitPublisher struct {
	channel *amqp.Channel
	mu      sync.Mutex
}

func NewRabbitPublisher(connection *amqp.Connection) (*RabbitPublisher, error) {
	channel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open publisher channel: %w", err)
	}
	if _, err := declareQueue(channel); err != nil {
		_ = channel.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	return &RabbitPublisher{channel: channel}, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, taskID uint64) error {
	return p.publish(ctx, taskID, 0)
}

func (p *RabbitPublisher) PublishRetry(ctx context.Context, taskID uint64, retryCount int) error {
	return p.publish(ctx, taskID, retryCount)
}

func (p *RabbitPublisher) publish(ctx context.Context, taskID uint64, retryCount int) error {
	body, err := json.Marshal(TaskMessage{TaskID: taskID})
	if err != nil {
		return fmt.Errorf("encode task message: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		"",
		QueueName,
		false,
		false,
		amqp.Publishing{
			Headers:      amqp.Table{retryHeader: int32(retryCount)},
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now().UTC(),
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish task message: %w", err)
	}
	confirmed, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		return fmt.Errorf("wait for task publish confirmation: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("RabbitMQ rejected task message")
	}
	return nil
}

func (p *RabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.channel.Close(); err != nil {
		return fmt.Errorf("close publisher channel: %w", err)
	}
	return nil
}

func declareQueue(channel *amqp.Channel) (amqp.Queue, error) {
	queue, err := channel.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("declare task queue: %w", err)
	}
	return queue, nil
}
