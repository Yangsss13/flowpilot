package executionlock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeRedisEntry struct {
	value     string
	expiresAt time.Time
}

type fakeRedisCommands struct {
	mu      sync.Mutex
	entries map[string]fakeRedisEntry
	setErr  error
	evalErr error
}

func newFakeRedisCommands() *fakeRedisCommands {
	return &fakeRedisCommands{entries: make(map[string]fakeRedisEntry)}
}

func (f *fakeRedisCommands) SetNX(_ context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return redis.NewBoolResult(false, f.setErr)
	}
	if entry, ok := f.entries[key]; ok && time.Now().Before(entry.expiresAt) {
		return redis.NewBoolResult(false, nil)
	}
	f.entries[key] = fakeRedisEntry{value: value.(string), expiresAt: time.Now().Add(expiration)}
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRedisCommands) Eval(_ context.Context, _ string, keys []string, args ...interface{}) *redis.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.evalErr != nil {
		return redis.NewCmdResult(nil, f.evalErr)
	}
	entry, ok := f.entries[keys[0]]
	if ok && entry.value == args[0].(string) {
		delete(f.entries, keys[0])
		return redis.NewCmdResult(int64(1), nil)
	}
	return redis.NewCmdResult(int64(0), nil)
}

func TestRedisTaskLockerRejectsConcurrentOwner(t *testing.T) {
	client := newFakeRedisCommands()
	locker, err := NewRedisTaskLocker(client, time.Minute)
	if err != nil {
		t.Fatalf("NewRedisTaskLocker() returned error: %v", err)
	}
	release, err := locker.Acquire(context.Background(), 7)
	if err != nil {
		t.Fatalf("first Acquire() returned error: %v", err)
	}
	if _, err := locker.Acquire(context.Background(), 7); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("second Acquire() error = %v, want ErrNotAcquired", err)
	}
	if err := release(context.Background()); err != nil {
		t.Fatalf("release returned error: %v", err)
	}
	if _, err := locker.Acquire(context.Background(), 7); err != nil {
		t.Fatalf("Acquire() after release returned error: %v", err)
	}
}

func TestRedisTaskLockerDoesNotDeleteAnotherOwner(t *testing.T) {
	client := newFakeRedisCommands()
	locker, err := NewRedisTaskLocker(client, time.Minute)
	if err != nil {
		t.Fatalf("NewRedisTaskLocker() returned error: %v", err)
	}
	release, err := locker.Acquire(context.Background(), 7)
	if err != nil {
		t.Fatalf("Acquire() returned error: %v", err)
	}

	client.mu.Lock()
	client.entries["minikvx:task:lock:7"] = fakeRedisEntry{value: "new-owner", expiresAt: time.Now().Add(time.Minute)}
	client.mu.Unlock()
	if err := release(context.Background()); err != nil {
		t.Fatalf("release returned error: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.entries["minikvx:task:lock:7"].value; got != "new-owner" {
		t.Fatalf("lock owner = %q, want new-owner", got)
	}
}

func TestRedisTaskLockerAllowsAcquireAfterExpiry(t *testing.T) {
	client := newFakeRedisCommands()
	locker, err := NewRedisTaskLocker(client, time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisTaskLocker() returned error: %v", err)
	}
	if _, err := locker.Acquire(context.Background(), 7); err != nil {
		t.Fatalf("first Acquire() returned error: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := locker.Acquire(context.Background(), 7); err != nil {
		t.Fatalf("Acquire() after expiry returned error: %v", err)
	}
}

func TestRedisTaskLockerRequiresPositiveTTL(t *testing.T) {
	if _, err := NewRedisTaskLocker(newFakeRedisCommands(), 0); err == nil {
		t.Fatal("NewRedisTaskLocker() returned nil error for zero TTL")
	}
}

func TestRedisTaskLockerReturnsRedisErrors(t *testing.T) {
	client := newFakeRedisCommands()
	client.setErr = errors.New("redis unavailable")
	locker, err := NewRedisTaskLocker(client, time.Minute)
	if err != nil {
		t.Fatalf("NewRedisTaskLocker() returned error: %v", err)
	}
	if _, err := locker.Acquire(context.Background(), 7); err == nil {
		t.Fatal("Acquire() returned nil error for Redis failure")
	}
}
