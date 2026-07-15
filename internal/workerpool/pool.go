package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

var ErrQueueFull = errors.New("worker queue is full")
var ErrClosed = errors.New("worker pool is closed")

type TaskRunner interface {
	Execute(ctx context.Context, taskID uint64) error
}

type job struct {
	taskID uint64
	ctx    context.Context
	result chan error
}

type Pool struct {
	runner TaskRunner
	jobs   chan job

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	mu      sync.RWMutex
	stopped bool
	stop    sync.Once
}

func New(parent context.Context, runner TaskRunner, workerCount, queueSize int) (*Pool, error) {
	if workerCount <= 0 {
		return nil, fmt.Errorf("worker count must be positive")
	}
	if queueSize <= 0 {
		return nil, fmt.Errorf("queue size must be positive")
	}

	ctx, cancel := context.WithCancel(parent)
	pool := &Pool{
		runner: runner,
		jobs:   make(chan job, queueSize),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	pool.wg.Add(workerCount)
	for workerID := 1; workerID <= workerCount; workerID++ {
		go pool.runWorker(workerID)
	}
	go func() {
		pool.wg.Wait()
		close(pool.done)
		pool.cancel()
	}()
	go func() {
		<-pool.ctx.Done()
		pool.closeQueue()
	}()

	return pool, nil
}

func (p *Pool) Submit(taskID uint64) error {
	return p.enqueue(job{taskID: taskID})
}

func (p *Pool) Execute(ctx context.Context, taskID uint64) error {
	result := make(chan error, 1)
	if err := p.enqueue(job{taskID: taskID, ctx: ctx, result: result}); err != nil {
		return err
	}

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return ErrClosed
	}
}

func (p *Pool) enqueue(next job) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stopped {
		return ErrClosed
	}

	select {
	case p.jobs <- next:
		return nil
	default:
		return ErrQueueFull
	}
}

func (p *Pool) Stop(ctx context.Context) error {
	p.closeQueue()

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.cancel()
		return fmt.Errorf("stop worker pool: %w", ctx.Err())
	}
}

func (p *Pool) closeQueue() {
	p.stop.Do(func() {
		p.mu.Lock()
		p.stopped = true
		close(p.jobs)
		p.mu.Unlock()
	})
}

func (p *Pool) runWorker(workerID int) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case next, ok := <-p.jobs:
			if !ok {
				return
			}
			if p.ctx.Err() != nil {
				return
			}
			runCtx := p.ctx
			cancel := func() {}
			stopPoolCancellation := func() bool { return false }
			if next.ctx != nil {
				runCtx, cancel = context.WithCancel(next.ctx)
				stopPoolCancellation = context.AfterFunc(p.ctx, cancel)
			}
			err := p.runner.Execute(runCtx, next.taskID)
			stopPoolCancellation()
			cancel()
			if next.result != nil {
				next.result <- err
				continue
			}
			if err != nil {
				log.Printf("worker %d execute task %d: %v", workerID, next.taskID, err)
			}
		}
	}
}
