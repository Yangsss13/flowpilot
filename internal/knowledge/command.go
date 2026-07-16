package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

type CommandRunner interface {
	Run(ctx context.Context, executable string, arguments []string, maxOutput int) ([]byte, error)
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, executable string, arguments []string, maxOutput int) ([]byte, error) {
	if maxOutput <= 0 {
		return nil, fmt.Errorf("command output limit must be positive")
	}
	if _, err := exec.LookPath(executable); err != nil {
		return nil, fmt.Errorf("%w: executable is not installed", ErrMediaRuntime)
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	output := &boundedBuffer{limit: maxOutput}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.Bytes(), fmt.Errorf("%w: %v", ErrMediaTimeout, ctx.Err())
		}
		return output.Bytes(), ctx.Err()
	}
	if output.exceeded {
		return output.Bytes(), fmt.Errorf("media command output exceeded %d bytes", maxOutput)
	}
	if err != nil {
		return output.Bytes(), fmt.Errorf("media command failed: %w", err)
	}
	return output.Bytes(), nil
}

type boundedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(value[:min(len(value), remaining)])
	}
	if len(value) > remaining {
		b.exceeded = true
	}
	return len(value), nil
}

func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

type limiter struct {
	tokens chan struct{}
}

func newLimiter(limit int) *limiter {
	if limit <= 0 {
		limit = 1
	}
	return &limiter{tokens: make(chan struct{}, limit)}
}

func (l *limiter) acquire(ctx context.Context) error {
	select {
	case l.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *limiter) release() {
	<-l.tokens
}
