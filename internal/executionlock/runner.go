package executionlock

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type TaskRunner interface {
	Execute(ctx context.Context, taskID uint64) error
}

type LockedTaskRunner struct {
	locker TaskLocker
	next   TaskRunner
}

func NewLockedTaskRunner(locker TaskLocker, next TaskRunner) *LockedTaskRunner {
	return &LockedTaskRunner{locker: locker, next: next}
}

func (r *LockedTaskRunner) Execute(ctx context.Context, taskID uint64) (resultErr error) {
	release, err := r.locker.Acquire(ctx, taskID)
	if err != nil {
		return fmt.Errorf("lock task %d: %w", taskID, err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, release(releaseCtx))
	}()

	return r.next.Execute(ctx, taskID)
}
