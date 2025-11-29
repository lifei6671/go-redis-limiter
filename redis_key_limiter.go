package limiter

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/go-redis/redis/v8"
)

var tokenBucketScript = redis.NewScript(`
	-- KEYS[1] = tokens key
	-- KEYS[2] = ts key
	-- ARGV[1] = rate/sec
	-- ARGV[2] = capacity
	-- ARGV[3] = now (ms)
	-- ARGV[4] = request tokens (usually 1)
	-- ARGV[5] = TTL (ms)
	
	local rate = tonumber(ARGV[1])
	local capacity = tonumber(ARGV[2])
	local now = tonumber(ARGV[3])
	local request = tonumber(ARGV[4])
	local ttl = tonumber(ARGV[5])
	
	local tokens = tonumber(redis.call("GET", KEYS[1]))
	local ts = tonumber(redis.call("GET", KEYS[2]))
	
	if tokens == nil then
		tokens = capacity
		ts = now
	else
		local delta = now - ts
		if delta > 0 then
			local refill = delta * rate / 1000
			tokens = math.min(capacity, tokens + refill)
			ts = now
		end
	end
	
	local allowed = 0
	if tokens >= request then
		tokens = tokens - request
		allowed = 1
	end
	
	redis.call("SET", KEYS[1], tokens, "PX", ttl)
	redis.call("SET", KEYS[2], ts, "PX", ttl)
	
	return allowed
`)

// ShardedRedisTokenBucket 支持 sharding 的分布式令牌桶限流器。
type ShardedRedisTokenBucket struct {
	client   *redis.Client
	name     string        // 逻辑限流器名称，例如 "api-login"
	rate     float64       // 全局总速率（tokens/sec）
	capacity float64       // 全局总容量
	shards   int           // 分片数量
	ttl      time.Duration // Redis key TTL
}

// NewShardedRedisTokenBucket 创建一个带分片的令牌桶。
// rate/capacity 是全局值，内部会自动按 shard 数拆分。
func NewShardedRedisTokenBucket(
	client *redis.Client,
	name string,
	rate float64,
	capacity int64,
	shards int,
	ttl time.Duration,
) *ShardedRedisTokenBucket {
	if shards <= 0 {
		shards = 1
	}
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &ShardedRedisTokenBucket{
		client:   client,
		name:     name,
		rate:     rate,
		capacity: float64(capacity),
		shards:   shards,
		ttl:      ttl,
	}
}

// shardFor 根据 shardKey 选择一个 shard。
// shardKey 可以是 userID、IP、session 等。
// 如果你只想做全局限流，可以传固定字符串，比如 "global"。
func (l *ShardedRedisTokenBucket) shardFor(shardKey string) int {
	if l.shards == 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(shardKey))
	return int(h.Sum32() % uint32(l.shards))
}

// buildKeys 构造某个 shard 的 Redis key。
// 使用 hash tag 保证同一 shard 的 tokens/ts 落在同一 slot。
// 同时不同 shard（name:0,name:1,...) 可以分散到不同 slot。
func (l *ShardedRedisTokenBucket) buildKeys(shardIdx int) (tokensKey, tsKey string) {
	// {name:shardIdx} 作为 hash tag，确保 cluster 中可以打散 shard。
	hashTag := fmt.Sprintf("{%s:%d}", l.name, shardIdx)
	tokensKey = "limiter:" + hashTag + ":tokens"
	tsKey = "limiter:" + hashTag + ":ts"
	return
}

// Allow 对某个 shardKey 做一次尝试，不等待。
// 返回是否允许。
func (l *ShardedRedisTokenBucket) Allow(ctx context.Context, shardKey string) (bool, error) {
	shardIdx := l.shardFor(shardKey)

	// 按 shard 拆分令牌桶配置
	shardRate := l.rate / float64(l.shards)
	shardCapacity := l.capacity / float64(l.shards)

	if shardRate <= 0 {
		shardRate = 1
	}
	if shardCapacity <= 0 {
		shardCapacity = 1
	}

	nowMs := float64(time.Now().UnixNano() / 1e6)
	tokensKey, tsKey := l.buildKeys(shardIdx)

	res, err := tokenBucketScript.Run(ctx, l.client,
		[]string{tokensKey, tsKey},
		shardRate,
		shardCapacity,
		nowMs,
		1, // 每次请求消耗一个 token
		l.ttl.Milliseconds(),
	).Result()
	if err != nil {
		return false, err
	}

	allowed, ok := res.(int64)
	if !ok {
		// 理论上不会出现，防御性兜底
		return false, fmt.Errorf("unexpected redis script result: %#v", res)
	}
	return allowed == 1, nil
}

// Wait 带等待的限流。
// maxWait 是最多等待时间；shardKey 决定分片。
func (l *ShardedRedisTokenBucket) Wait(ctx context.Context, shardKey string, maxWait time.Duration) error {
	if maxWait <= 0 {
		maxWait = 0
	}

	deadline := time.Now().Add(maxWait)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		ok, err := l.Allow(ctx, shardKey)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		if maxWait == 0 {
			// 不等待，直接返回限流
			return ErrLimiter
		}

		now := time.Now()
		if now.After(deadline) {
			return ErrTimeout
		}

		// 计算一个合理的 sleep 间隔，避免惊群
		// roughly 1 / rate 秒，最低 1ms
		shardRate := l.rate / float64(l.shards)
		if shardRate <= 0 {
			shardRate = 1
		}
		sleep := time.Duration(float64(time.Second) / shardRate)
		if sleep < time.Millisecond {
			sleep = time.Millisecond
		}
		remain := time.Until(deadline)
		// 根据给定的最大超时事件，计算需要等待的时间，这里不能直接用传入的等待时长，需要在等待时间内多次重试。
		if sleep > remain {
			sleep = remain
		}
		timer.Reset(sleep)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			// 这里等待一段时间，方式频繁访问 Redis
		}
	}
}

type WrapperLimiter struct {
	tb       *ShardedRedisTokenBucket
	shardKey string
	maxWait  time.Duration
}

func NewWrapperLimiter(tb *ShardedRedisTokenBucket, shardKey string, maxWait time.Duration) Limiter {
	return &WrapperLimiter{
		tb:       tb,
		shardKey: shardKey,
		maxWait:  maxWait,
	}
}

func (w *WrapperLimiter) Wait(ctx context.Context) error {
	return w.tb.Wait(ctx, w.shardKey, w.maxWait)
}

func (w *WrapperLimiter) Done(ctx context.Context) {
	// 令牌桶不需要 Done，这里留空即可
}
