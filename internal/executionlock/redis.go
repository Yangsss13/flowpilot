package executionlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrNotAcquired = errors.New("task execution lock is already held")

const releaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`

type redisCommands interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

type ReleaseFunc func(ctx context.Context) error

type TaskLocker interface {
	Acquire(ctx context.Context, taskID uint64) (ReleaseFunc, error)
}

type RedisTaskLocker struct {
	client redisCommands
	ttl    time.Duration
}

func NewRedisTaskLocker(client redisCommands, ttl time.Duration) (*RedisTaskLocker, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("lock TTL must be positive")
	}
	return &RedisTaskLocker{client: client, ttl: ttl}, nil
}

func (l *RedisTaskLocker) Acquire(ctx context.Context, taskID uint64) (ReleaseFunc, error) {
	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate lock token: %w", err)
	}
	key := fmt.Sprintf("flowpilot:task:lock:%d", taskID)
	acquired, err := l.client.SetNX(ctx, key, token, l.ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire task lock: %w", err)
	}
	if !acquired {
		return nil, ErrNotAcquired
	}

	return func(ctx context.Context) error {
		if err := l.client.Eval(ctx, releaseScript, []string{key}, token).Err(); err != nil {
			return fmt.Errorf("release task lock: %w", err)
		}
		return nil
	}, nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
