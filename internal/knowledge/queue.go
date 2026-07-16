package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const QueueName = "flowpilot.knowledge.ingestion"

type JobMessage struct {
	JobID uint64 `json:"job_id"`
}

type RabbitPublisher struct {
	channel *amqp.Channel
	mu      sync.Mutex
}

func NewRabbitPublisher(connection *amqp.Connection) (*RabbitPublisher, error) {
	channel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open knowledge publisher channel: %w", err)
	}
	if _, err := declareKnowledgeQueue(channel); err != nil {
		_ = channel.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("enable knowledge publisher confirms: %w", err)
	}
	return &RabbitPublisher{channel: channel}, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, jobID uint64) error {
	if jobID == 0 {
		return fmt.Errorf("job id must be positive")
	}
	body, err := json.Marshal(JobMessage{JobID: jobID})
	if err != nil {
		return fmt.Errorf("encode knowledge job: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(publishCtx, "", QueueName, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent,
		Timestamp: time.Now().UTC(), Body: body,
	})
	if err != nil {
		return fmt.Errorf("publish knowledge job: %w", err)
	}
	confirmed, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		return fmt.Errorf("wait for knowledge publish confirmation: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("RabbitMQ rejected knowledge job")
	}
	return nil
}

func (p *RabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.channel.Close()
}

func declareKnowledgeQueue(channel *amqp.Channel) (amqp.Queue, error) {
	queue, err := channel.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("declare knowledge queue: %w", err)
	}
	return queue, nil
}

type JobProcessor interface {
	Process(ctx context.Context, jobID uint64) error
}

type Consumer struct {
	connection *amqp.Connection
	processor  JobProcessor
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	channels   []*amqp.Channel
}

func NewConsumer(connection *amqp.Connection, processor JobProcessor) *Consumer {
	return &Consumer{connection: connection, processor: processor}
}

func (c *Consumer) Start(parent context.Context, workerCount int) error {
	if workerCount <= 0 {
		return fmt.Errorf("knowledge worker count must be positive")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return fmt.Errorf("knowledge consumer already started")
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
			for delivery := range messages {
				if err := c.handle(ctx, delivery); err != nil {
					log.Printf("knowledge consumer %d: %v", id, err)
				}
			}
		}(workerID, deliveries)
	}
	go func() {
		workers.Wait()
		close(c.done)
	}()
	return nil
}

func (c *Consumer) openWorker(ctx context.Context, workerID int) (*amqp.Channel, <-chan amqp.Delivery, error) {
	channel, err := c.connection.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("open knowledge consumer %d: %w", workerID, err)
	}
	if _, err := declareKnowledgeQueue(channel); err != nil {
		_ = channel.Close()
		return nil, nil, err
	}
	if err := channel.Qos(1, 0, false); err != nil {
		_ = channel.Close()
		return nil, nil, fmt.Errorf("set knowledge consumer QoS: %w", err)
	}
	deliveries, err := channel.ConsumeWithContext(ctx, QueueName, fmt.Sprintf("flowpilot-knowledge-%d", workerID), false, false, false, false, nil)
	if err != nil {
		_ = channel.Close()
		return nil, nil, fmt.Errorf("consume knowledge queue: %w", err)
	}
	return channel, deliveries, nil
}

func (c *Consumer) handle(ctx context.Context, delivery amqp.Delivery) error {
	var message JobMessage
	if err := json.Unmarshal(delivery.Body, &message); err != nil || message.JobID == 0 {
		return delivery.Ack(false)
	}
	err := c.processor.Process(ctx, message.JobID)
	if err == nil {
		return delivery.Ack(false)
	}
	if errors.Is(err, ErrRetryJob) {
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		return delivery.Nack(false, true)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return delivery.Nack(false, true)
	}
	if nackErr := delivery.Nack(false, true); nackErr != nil {
		return errors.Join(err, nackErr)
	}
	return err
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
		return fmt.Errorf("stop knowledge consumer: %w", ctx.Err())
	}
}

type Dispatcher struct {
	repository *GormRepository
	publisher  JobPublisher
	worker     *Worker
	interval   time.Duration
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewDispatcher(repository *GormRepository, publisher JobPublisher, worker *Worker, interval time.Duration) *Dispatcher {
	return &Dispatcher{repository: repository, publisher: publisher, worker: worker, interval: interval}
}

func (d *Dispatcher) Start(parent context.Context) error {
	if d.interval <= 0 {
		return fmt.Errorf("knowledge dispatch interval must be positive")
	}
	if err := d.repository.RecoverInterrupted(parent); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	d.cancel = cancel
	d.done = make(chan struct{})
	if err := d.dispatch(ctx); err != nil {
		log.Printf("initial knowledge dispatch: %v", err)
	}
	go func() {
		defer close(d.done)
		ticker := time.NewTicker(d.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := d.dispatch(ctx); err != nil {
					log.Printf("knowledge dispatch: %v", err)
				}
			}
		}
	}()
	return nil
}

func (d *Dispatcher) dispatch(ctx context.Context) error {
	ids, err := d.repository.ListQueuedJobIDs(ctx, 100)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := d.publisher.Publish(ctx, id); err != nil {
			return err
		}
		if err := d.repository.MarkJobDispatched(ctx, id); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
	}
	if err := d.worker.CleanupDeleting(ctx, 20); err != nil {
		return err
	}
	if err := d.worker.CleanupCanceled(ctx, 50); err != nil {
		return err
	}
	return d.worker.CleanupSuperseded(ctx, 100)
}

func (d *Dispatcher) Stop(ctx context.Context) error {
	if d.cancel == nil {
		return nil
	}
	d.cancel()
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
