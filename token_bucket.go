package limiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

// TokenBucketLimiter 是一个“单桶令牌桶”限流器。
// 特点：
//   - 允许突发，平均速率约为 Rate token/sec
//   - 令牌用完后会被限流，直到补充足够的 token
//   - 适合 API QPS 限制、任务消费速率限制等场景。
type TokenBucketLimiter struct {
	client *redis.Client

	Key    string // 业务 key，例如 "api:/v1/login"、"user:123"
	Prefix string // Redis key 前缀，默认 "tbucket"

	Rate     float64       // token 生成速率，单位：token/sec
	Capacity float64       // 桶容量（最大 token 数）
	TTL      time.Duration // Redis key 过期时间，建议略大于典型空闲时间
}

// NewTokenBucketLimiter 创建一个单桶令牌桶限流器。
// 配置通过 TokenBucketOption 传入，避免参数爆炸。
//   - client: go-redis 客户端
//   - key:    限流业务 key
//   - opts:   配置项（Rate、Capacity、TTL、Prefix）
func NewTokenBucketLimiter(
	client *redis.Client,
	key string,
	opts ...TokenBucketOption,
) *TokenBucketLimiter {

	if client == nil {
		panic("token bucket: redis client is nil")
	}
	if key == "" {
		panic("token bucket: key is empty")
	}

	tb := &TokenBucketLimiter{
		client:   client,
		Key:      key,
		Prefix:   "tbucket",
		Rate:     100,             // 默认速率：100 token/sec
		Capacity: 100,             // 默认容量：100
		TTL:      2 * time.Second, // 默认 TTL：2 秒
	}

	for _, opt := range opts {
		opt(tb)
	}
	return tb
}

// tokensKey 返回当前 token 数对应的 Redis key。
// 使用 hash tag {Key}，保证在 Redis Cluster 中相关 key 落在同一个 slot。
func (tb *TokenBucketLimiter) tokensKey() string {
	return fmt.Sprintf("%s:{%s}:tokens", tb.Prefix, tb.Key)
}

// tsKey 返回记录上次更新时间的 Redis key。
func (tb *TokenBucketLimiter) tsKey() string {
	return fmt.Sprintf("%s:{%s}:ts", tb.Prefix, tb.Key)
}

// Allow 尝试获取 1 个 token。
func (tb *TokenBucketLimiter) Allow(ctx context.Context) (bool, error) {
	return tb.AllowN(ctx, 1)
}

// AllowN 尝试一次获取 n 个 token。
func (tb *TokenBucketLimiter) AllowN(ctx context.Context, n int64) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("token bucket: n must > 0")
	}

	nowMs := float64(time.Now().UnixNano() / 1e6)
	ttlMs := tb.TTL.Milliseconds()

	res, err := tokenBucketScript.Run(
		ctx,
		tb.client,
		[]string{tb.tokensKey(), tb.tsKey()},
		nowMs,
		tb.Rate,
		tb.Capacity,
		float64(n),
		ttlMs,
	).Result()
	if err != nil {
		return false, err
	}

	switch v := res.(type) {
	case int64:
		return v == 1, nil
	case int:
		return int64(v) == 1, nil
	default:
		return false, fmt.Errorf("token bucket: unexpected script result: %#v", res)
	}
}

// Wait 阻塞直到成功获取 1 个 token 或 ctx 取消。
// 实现策略：循环调用 Allow，若被限流则 sleep 一小段时间。
func (tb *TokenBucketLimiter) Wait(ctx context.Context, maxWait time.Duration) error {
	maxWait = max(maxWait, 0)

	deadline := time.Now().Add(maxWait)

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		ok, err := tb.Allow(ctx)
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
		sleep := 10 * time.Millisecond
		remain := time.Until(deadline)
		if sleep > remain {
			sleep = remain
		}
		timer.Reset(sleep)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// State 返回当前令牌桶的状态。
// 这里会从 Redis 读出 tokens 和 ts，并在本地模拟一次 refill，以获得“理论上的当前 token 数”。
func (tb *TokenBucketLimiter) State(ctx context.Context) (LimiterState, error) {
	tokensStr, err := tb.client.Get(ctx, tb.tokensKey()).Result()
	if errors.Is(err, redis.Nil) {
		// 桶未初始化，视为“满桶”状态
		now := time.Now().UnixMilli()
		return LimiterState{
			Level:             tb.Capacity,
			Remaining:         tb.Capacity,
			Capacity:          tb.Capacity,
			Rate:              tb.Rate,
			LastUpdated:       now,
			NextAvailableTime: now,
			Type:              "token_bucket",
			Key:               tb.Key,
		}, nil
	}
	if err != nil {
		return LimiterState{}, err
	}

	tsStr, err := tb.client.Get(ctx, tb.tsKey()).Result()
	if err != nil {
		return LimiterState{}, err
	}

	tokens, err := strconv.ParseFloat(tokensStr, 64)
	if err != nil {
		return LimiterState{}, fmt.Errorf("token bucket: invalid tokens: %v", err)
	}
	lastTs, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return LimiterState{}, fmt.Errorf("token bucket: invalid ts: %v", err)
	}

	now := time.Now()
	nowMs := now.UnixNano() / 1e6
	deltaMs := float64(nowMs - lastTs)
	if deltaMs < 0 {
		deltaMs = 0
	}

	// 在本地模拟 refill
	refill := (deltaMs * tb.Rate) / 1000
	tokens += refill
	if tokens > tb.Capacity {
		tokens = tb.Capacity
	}

	// 对于令牌桶，我们把“可用 token 数”作为 Level/Remaining
	level := tokens
	if level < 0 {
		level = 0
	}

	// 下一次可用时间：如果当前 token >= 1，则现在即可。
	// 否则需要计算补足到 1 个 token 所需时间。
	var next time.Time
	if level >= 1 {
		next = now
	} else {
		need := 1 - level
		waitSec := need / tb.Rate
		if waitSec < 0 {
			waitSec = 0
		}
		next = now.Add(time.Duration(waitSec * float64(time.Second)))
	}

	return LimiterState{
		Level:             level,
		Remaining:         level,
		Capacity:          tb.Capacity,
		Rate:              tb.Rate,
		LastUpdated:       lastTs,
		NextAvailableTime: next.UnixMilli(),
		Type:              "token_bucket",
		Key:               tb.Key,
	}, nil
}
