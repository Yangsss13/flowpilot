package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingRunner struct {
	started chan uint64
	release chan struct{}
	active  atomic.Int32
	max     atomic.Int32
}

func (r *blockingRunner) Execute(ctx context.Context, taskID uint64) error {
	active := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		maximum := r.max.Load()
		if active <= maximum || r.max.CompareAndSwap(maximum, active) {
			break
		}
	}
	r.started <- taskID

	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPoolLimitsConcurrency(t *testing.T) {
	runner := &blockingRunner{
		started: make(chan uint64, 4),
		release: make(chan struct{}),
	}
	pool, err := New(context.Background(), runner, 2, 4)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer stopPool(t, pool)

	for id := uint64(1); id <= 4; id++ {
		if err := pool.Submit(id); err != nil {
			t.Fatalf("Submit(%d) returned error: %v", id, err)
		}
	}

	waitForStarts(t, runner.started, 2)
	select {
	case id := <-runner.started:
		t.Fatalf("third task %d started before a worker was released", id)
	case <-time.After(50 * time.Millisecond):
	}
	if got := runner.max.Load(); got != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", got)
	}
	close(runner.release)
}

func TestPoolReturnsQueueFullWithoutBlocking(t *testing.T) {
	runner := &blockingRunner{
		started: make(chan uint64, 1),
		release: make(chan struct{}),
	}
	pool, err := New(context.Background(), runner, 1, 1)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer stopPool(t, pool)

	if err := pool.Submit(1); err != nil {
		t.Fatalf("Submit(1) returned error: %v", err)
	}
	waitForStarts(t, runner.started, 1)
	if err := pool.Submit(2); err != nil {
		t.Fatalf("Submit(2) returned error: %v", err)
	}

	started := time.Now()
	err = pool.Submit(3)
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Submit(3) error = %v, want ErrQueueFull", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("full queue response took %s, want under 100ms", elapsed)
	}
	close(runner.release)
}

func TestPoolStopCancelsRunningTask(t *testing.T) {
	runner := &blockingRunner{
		started: make(chan uint64, 1),
		release: make(chan struct{}),
	}
	pool, err := New(context.Background(), runner, 1, 1)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if err := pool.Submit(1); err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}
	waitForStarts(t, runner.started, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = pool.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want context deadline exceeded", err)
	}

	if err := pool.Submit(2); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit() after Stop error = %v, want ErrClosed", err)
	}
}

type recordingRunner struct {
	mu       sync.Mutex
	executed []uint64
}

func (r *recordingRunner) Execute(_ context.Context, taskID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executed = append(r.executed, taskID)
	return nil
}

func TestPoolStopDrainsQueuedTasks(t *testing.T) {
	runner := &recordingRunner{}
	pool, err := New(context.Background(), runner, 1, 4)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	for id := uint64(1); id <= 4; id++ {
		if err := pool.Submit(id); err != nil {
			t.Fatalf("Submit(%d) returned error: %v", id, err)
		}
	}

	stopPool(t, pool)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.executed) != 4 {
		t.Fatalf("executed tasks = %v, want all 4 tasks", runner.executed)
	}
}

func TestPoolSubmitConcurrentWithStop(t *testing.T) {
	runner := &recordingRunner{}
	pool, err := New(context.Background(), runner, 2, 16)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	const submitters = 16
	var ready sync.WaitGroup
	var finished sync.WaitGroup
	start := make(chan struct{})
	ready.Add(submitters)
	finished.Add(submitters)
	errorsFound := make(chan error, submitters)
	for i := 0; i < submitters; i++ {
		go func(id int) {
			defer finished.Done()
			ready.Done()
			<-start
			for attempt := 0; attempt < 1000; attempt++ {
				err := pool.Submit(uint64(id + 1))
				if err == nil || errors.Is(err, ErrQueueFull) {
					continue
				}
				if errors.Is(err, ErrClosed) {
					return
				}
				errorsFound <- err
				return
			}
		}(i)
	}
	ready.Wait()
	close(start)

	stopPool(t, pool)
	finished.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("Submit() returned unexpected error: %v", err)
	}
}

func waitForStarts(t *testing.T, started <-chan uint64, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for task %d to start", i+1)
		}
	}
}

func stopPool(t *testing.T, pool *Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Stop(ctx); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
}
