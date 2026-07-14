package executionlock

import (
	"context"
	"errors"
	"testing"
)

type fakeTaskLocker struct {
	err      error
	released bool
}

func (f *fakeTaskLocker) Acquire(_ context.Context, _ uint64) (ReleaseFunc, error) {
	if f.err != nil {
		return nil, f.err
	}
	return func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.released = true
		return nil
	}, nil
}

type fakeTaskRunner struct {
	calls  int
	err    error
	cancel context.CancelFunc
}

func (f *fakeTaskRunner) Execute(_ context.Context, _ uint64) error {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return f.err
}

func TestLockedTaskRunnerExecutesAndReleases(t *testing.T) {
	locker := &fakeTaskLocker{}
	next := &fakeTaskRunner{}
	runner := NewLockedTaskRunner(locker, next)
	if err := runner.Execute(context.Background(), 7); err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if next.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", next.calls)
	}
	if !locker.released {
		t.Fatal("lock was not released")
	}
}

func TestLockedTaskRunnerDoesNotExecuteWithoutLock(t *testing.T) {
	locker := &fakeTaskLocker{err: ErrNotAcquired}
	next := &fakeTaskRunner{}
	runner := NewLockedTaskRunner(locker, next)
	err := runner.Execute(context.Background(), 7)
	if !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("Execute() error = %v, want ErrNotAcquired", err)
	}
	if next.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", next.calls)
	}
}

func TestLockedTaskRunnerReleasesAfterExecutionError(t *testing.T) {
	locker := &fakeTaskLocker{}
	next := &fakeTaskRunner{err: errors.New("execution failed")}
	runner := NewLockedTaskRunner(locker, next)
	if err := runner.Execute(context.Background(), 7); err == nil {
		t.Fatal("Execute() returned nil error")
	}
	if !locker.released {
		t.Fatal("lock was not released after execution error")
	}
}

func TestLockedTaskRunnerReleasesWithCancelledExecutionContext(t *testing.T) {
	locker := &fakeTaskLocker{}
	ctx, cancel := context.WithCancel(context.Background())
	next := &fakeTaskRunner{err: context.Canceled, cancel: cancel}
	runner := NewLockedTaskRunner(locker, next)
	if err := runner.Execute(ctx, 7); err == nil {
		t.Fatal("Execute() returned nil error")
	}
	if !locker.released {
		t.Fatal("lock was not released with cancelled execution context")
	}
}
