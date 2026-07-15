package taskqueue

import (
	"context"
	"errors"
	"fmt"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yangsss13/flowpilot/internal/executionlock"
	"github.com/Yangsss13/flowpilot/internal/executor"
	"github.com/Yangsss13/flowpilot/internal/repository"
)

type fakeAcknowledger struct {
	acked        int
	nacked       int
	nackRequeued bool
}

func (f *fakeAcknowledger) Ack(_ uint64, _ bool) error {
	f.acked++
	return nil
}

func (f *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	f.nacked++
	f.nackRequeued = requeue
	return nil
}

func (f *fakeAcknowledger) Reject(_ uint64, _ bool) error {
	return nil
}

type fakeRunner struct {
	calls int
	err   error
}

func (f *fakeRunner) Execute(_ context.Context, _ uint64) error {
	f.calls++
	return f.err
}

type fakeRetryPublisher struct {
	taskID     uint64
	retryCount int
	err        error
}

func (f *fakeRetryPublisher) PublishRetry(_ context.Context, taskID uint64, retryCount int) error {
	f.taskID = taskID
	f.retryCount = retryCount
	return f.err
}

func TestConsumerAcknowledgesSuccessfulTask(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{runner: &fakeRunner{}, publisher: &fakeRetryPublisher{}, maxRetries: 3}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 0, acknowledger)); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if acknowledger.acked != 1 || acknowledger.nacked != 0 {
		t.Fatalf("acked = %d, nacked = %d; want 1, 0", acknowledger.acked, acknowledger.nacked)
	}
}

func TestConsumerAcknowledgesDuplicateWithoutExecutingRetry(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{
		runner:     &fakeRunner{err: executionlock.ErrNotAcquired},
		publisher:  publisher,
		maxRetries: 3,
	}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 0, acknowledger)); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if acknowledger.acked != 1 || publisher.retryCount != 0 {
		t.Fatalf("acked = %d, published retry = %d; want 1, 0", acknowledger.acked, publisher.retryCount)
	}
}

func TestConsumerAcknowledgesFinalBusinessErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "task not runnable", err: executor.ErrTaskNotRunnable},
		{name: "step failed", err: executor.ErrStepExecution},
		{name: "state conflict", err: repository.ErrStateConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acknowledger := &fakeAcknowledger{}
			publisher := &fakeRetryPublisher{}
			consumer := &Consumer{
				runner:     &fakeRunner{err: tt.err},
				publisher:  publisher,
				maxRetries: 3,
			}
			if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 0, acknowledger)); err != nil {
				t.Fatalf("handleDelivery() returned error: %v", err)
			}
			if acknowledger.acked != 1 || acknowledger.nacked != 0 || publisher.retryCount != 0 {
				t.Fatalf("acked = %d, nacked = %d, retry = %d; want 1, 0, 0", acknowledger.acked, acknowledger.nacked, publisher.retryCount)
			}
		})
	}
}

func TestConsumerRepublishesTransientFailureThenAcknowledgesOriginal(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{
		runner:     &fakeRunner{err: errors.New("database unavailable")},
		publisher:  publisher,
		maxRetries: 3,
	}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 1, acknowledger)); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if publisher.taskID != 7 || publisher.retryCount != 2 {
		t.Fatalf("published task = %d retry = %d; want 7 and 2", publisher.taskID, publisher.retryCount)
	}
	if acknowledger.acked != 1 || acknowledger.nacked != 0 {
		t.Fatalf("acked = %d, nacked = %d; want 1, 0", acknowledger.acked, acknowledger.nacked)
	}
}

func TestConsumerRequeuesOriginalWhenRetryPublishFails(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{
		runner:     &fakeRunner{err: errors.New("database unavailable")},
		publisher:  &fakeRetryPublisher{err: errors.New("RabbitMQ unavailable")},
		maxRetries: 3,
	}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 0, acknowledger)); err == nil {
		t.Fatal("handleDelivery() returned nil error")
	}
	if acknowledger.nacked != 1 || !acknowledger.nackRequeued || acknowledger.acked != 0 {
		t.Fatalf("acked = %d, nacked = %d, requeue = %v; want 0, 1, true", acknowledger.acked, acknowledger.nacked, acknowledger.nackRequeued)
	}
}

func TestConsumerAcknowledgesAfterRetryLimit(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{
		runner:     &fakeRunner{err: errors.New("database unavailable")},
		publisher:  publisher,
		maxRetries: 3,
	}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 3, acknowledger)); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if acknowledger.acked != 1 || publisher.retryCount != 0 {
		t.Fatalf("acked = %d, published retry = %d; want 1, 0", acknowledger.acked, publisher.retryCount)
	}
}

func TestConsumerRequeuesCancelledExecution(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{
		runner:     &fakeRunner{err: context.Canceled},
		publisher:  &fakeRetryPublisher{},
		maxRetries: 3,
	}
	if err := consumer.handleDelivery(context.Background(), taskDelivery(7, 0, acknowledger)); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if acknowledger.nacked != 1 || !acknowledger.nackRequeued {
		t.Fatalf("nacked = %d, requeue = %v; want 1, true", acknowledger.nacked, acknowledger.nackRequeued)
	}
}

func TestConsumerDiscardsInvalidMessage(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1, Body: []byte(`{"task_id":0}`)}
	consumer := &Consumer{runner: &fakeRunner{}, publisher: &fakeRetryPublisher{}, maxRetries: 3}
	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery() returned error: %v", err)
	}
	if acknowledger.acked != 1 {
		t.Fatalf("acked = %d, want 1", acknowledger.acked)
	}
}

func taskDelivery(taskID uint64, retryCount int32, acknowledger amqp.Acknowledger) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  1,
		Headers:      amqp.Table{retryHeader: retryCount},
		Body:         []byte(`{"task_id":` + fmt.Sprint(taskID) + `}`),
	}
}
