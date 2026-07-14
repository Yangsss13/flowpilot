package executionlock_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
	"minikvx-agent/internal/executionlock"
)

type blockingCountingRunner struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (r *blockingCountingRunner) Execute(_ context.Context, _ uint64) error {
	r.calls.Add(1)
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	return nil
}

func TestRedisTaskLockerConcurrentAcquire(t *testing.T) {
	if os.Getenv("MINIKVX_INTEGRATION") != "1" {
		t.Skip("set MINIKVX_INTEGRATION=1 to run Redis integration tests")
	}

	client, err := database.OpenRedis(config.Load().Redis)
	if err != nil {
		t.Fatalf("open Redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	locker, err := executionlock.NewRedisTaskLocker(client, time.Minute)
	if err != nil {
		t.Fatalf("create task locker: %v", err)
	}
	taskID := uint64(time.Now().UnixNano())
	key := fmt.Sprintf("minikvx:task:lock:%d", taskID)
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	next := &blockingCountingRunner{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	runner := executionlock.NewLockedTaskRunner(locker, next)

	const contenders = 10
	start := make(chan struct{})
	results := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		go func() {
			<-start
			results <- runner.Execute(context.Background(), taskID)
		}()
	}
	close(start)
	select {
	case <-next.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the winning runner")
	}

	executed := 0
	rejected := 0
	var unexpectedErr error
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for i := 0; i < contenders-1; i++ {
		var err error
		select {
		case err = <-results:
		case <-deadline.C:
			close(next.release)
			t.Fatal("timed out waiting for rejected contenders")
		}
		switch {
		case err == nil:
			executed++
		case errors.Is(err, executionlock.ErrNotAcquired):
			rejected++
		default:
			unexpectedErr = err
		}
	}
	close(next.release)
	if err := <-results; err == nil {
		executed++
	} else if errors.Is(err, executionlock.ErrNotAcquired) {
		rejected++
	} else {
		unexpectedErr = err
	}
	if unexpectedErr != nil {
		t.Fatalf("Execute() returned unexpected error: %v", unexpectedErr)
	}

	if executed != 1 || rejected != contenders-1 || next.calls.Load() != 1 {
		t.Fatalf("executed = %d, rejected = %d, runner calls = %d; want 1, %d, 1", executed, rejected, next.calls.Load(), contenders-1)
	}

	release, err := locker.Acquire(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Acquire() after winner released returned error: %v", err)
	}
	if err := release(context.Background()); err != nil {
		t.Fatalf("final release returned error: %v", err)
	}
}
