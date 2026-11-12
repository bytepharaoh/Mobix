package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local bucket = redis.call('HMGET', key, 'tokens', 'updated_at')
local tokens = tonumber(bucket[1])
local updated_at = tonumber(bucket[2])

if tokens == nil or updated_at == nil then
  tokens = capacity
  updated_at = now_ms
else
  local elapsed = math.max(0, now_ms - updated_at)
  tokens = math.min(capacity, tokens + (elapsed * capacity / window_ms))
end

local allowed = 0
local retry_after_ms = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry_after_ms = math.ceil((1 - tokens) * window_ms / capacity)
end

redis.call('HSET', key, 'tokens', tostring(tokens), 'updated_at', now_ms)
redis.call('PEXPIRE', key, math.ceil(window_ms * 2))
return {allowed, math.floor(tokens), retry_after_ms}
`

type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

type Backend interface {
	Allow(context.Context, string, Policy) (Decision, error)
}

type RedisBackend struct {
	client *redis.Client
	script *redis.Script
}

func OpenRedis(redisURL string) (*RedisBackend, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	options.DialTimeout = 500 * time.Millisecond
	options.ReadTimeout = 250 * time.Millisecond
	options.WriteTimeout = 250 * time.Millisecond
	options.PoolTimeout = 300 * time.Millisecond
	options.MaxRetries = 1
	return &RedisBackend{client: redis.NewClient(options), script: redis.NewScript(tokenBucketScript)}, nil
}

func (backend *RedisBackend) Allow(ctx context.Context, key string, policy Policy) (Decision, error) {
	values, err := backend.script.Run(ctx, backend.client, []string{key}, policy.Capacity, policy.Window.Milliseconds()).Slice()
	if err != nil {
		return Decision{}, err
	}
	if len(values) != 3 {
		return Decision{}, fmt.Errorf("rate limit script returned %d values", len(values))
	}
	allowed, okAllowed := values[0].(int64)
	remaining, okRemaining := values[1].(int64)
	retryMilliseconds, okRetry := values[2].(int64)
	if !okAllowed || !okRemaining || !okRetry {
		return Decision{}, fmt.Errorf("rate limit script returned unexpected value types")
	}
	return Decision{
		Allowed: allowed == 1, Remaining: remaining,
		RetryAfter: time.Duration(retryMilliseconds) * time.Millisecond,
	}, nil
}

func (backend *RedisBackend) Close() error { return backend.client.Close() }
