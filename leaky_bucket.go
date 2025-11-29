package limiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

// LeakyBucketLimiter 实现了经典的“漏桶限流”算法。
// 特点：
//   - 适合“平滑流量整形”（strict rate），严格控制输出速率
//   - 对突发流量不敏感（会被排队/丢弃），相比令牌桶更“匀速”
//   - 基于 Redis + Lua，支持分布式场景
type LeakyBucketLimiter struct {
	client *redis.Client

	Key    string // 业务维度限流 key，例如 "api:/v1/login"、"user:123"
	Prefix string // Redis key 前缀，默认 "lb"
	// LeakRate 泄漏速率：单位/秒（例如每秒“漏掉”多少请求）
	LeakRate float64
	// Capacity 桶容量：最大可堆积多少单位（例如最大队列长度）
	Capacity float64
	// TTL Redis key 过期时间：建议 >= “等价时间窗口”的 2 倍
	TTL time.Duration
}

// NewLeakyBucketLimiter 创建一个“单桶”的漏桶限流器。
// 必填：client, key
// 其他参数通过 Option 传入，提供合理默认值。
func NewLeakyBucketLimiter(
	client *redis.Client,
	key string,
	opts ...LeakyBucketOption,
) *LeakyBucketLimiter {

	if client == nil {
		panic("leaky bucket: redis client is nil")
	}
	if key == "" {
		panic("leaky bucket: key is empty")
	}

	l := &LeakyBucketLimiter{
		client:   client,
		Key:      key,
		Prefix:   "lb",
		LeakRate: 100,             // 默认每秒泄漏100单位
		Capacity: 100,             // 默认桶容量100
		TTL:      2 * time.Second, // 默认TTL
	}

	for _, opt := range opts {
		opt(l)
	}
	return l
}

// bucketKey 返回存储水位的 Redis key。
// 使用 {key} 作为 hash tag，保证 Redis Cluster 中 level 和 ts 落在同一 slot。
func (l *LeakyBucketLimiter) bucketKey() string {
	return fmt.Sprintf("%s:{%s}:bucket", l.Prefix, l.Key)
}

// tsKey 返回存储上次更新时间的 Redis key。
func (l *LeakyBucketLimiter) tsKey() string {
	return fmt.Sprintf("%s:{%s}:ts", l.Prefix, l.Key)
}

// Allow 尝试获取一个“许可”(1单位)，返回是否允许。
func (l *LeakyBucketLimiter) Allow(ctx context.Context) (bool, error) {
	return l.AllowN(ctx, 1)
}

// AllowN 尝试获取 n 个许可。
// 对漏桶来说，相当于往桶里加 n 单位的水。
func (l *LeakyBucketLimiter) AllowN(ctx context.Context, n int64) (bool, error) {
	if n <= 0 {
		return false, fmt.Errorf("leaky bucket: n must > 0")
	}

	nowMs := float64(time.Now().UnixNano() / 1e6)
	ttlMs := l.TTL.Milliseconds()

	res, err := leakyBucketScript.Run(
		ctx,
		l.client,
		[]string{l.bucketKey(), l.tsKey()},
		nowMs,
		l.LeakRate,
		l.Capacity,
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
		return false, fmt.Errorf("unexpected script result: %#v", res)
	}
}

// Wait 会阻塞直到成功获取一个许可或 ctx 超时/取消。
// 对漏桶来说，Wait 的语义是“等到桶里腾出空间为止”。
func (l *LeakyBucketLimiter) Wait(ctx context.Context, maxWait time.Duration) error {
	if maxWait <= 0 {
		maxWait = 0
	}
	deadline := time.Now().Add(maxWait)

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	for {
		ok, err := l.Allow(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		// 被限流时，简单 sleep 一小段时间，再重试。
		// 若要更精细，可以结合 State() 中的 NextAvailableTime 计算 sleep 时长。
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

// State 返回当前漏桶的状态，用于监控 / Debug。
//
// 这里不会修改 Redis 中的数据，而是在本地根据泄漏速率模拟“当前的真实水位”。
//
// Level            -> 当前真实水位（经过泄漏后的）
// Remaining        -> 桶剩余可用容量 = Capacity - Level
// Capacity         -> 桶容量
// Rate             -> 泄漏速率（LeakRate）
// LastUpdated      -> 上次更新时间（毫秒时间戳）
// NextAvailableTime-> 如果当前满了，预计下次可放行的时间
// Type             -> "leaky_bucket"
// Key              -> 限流 key
func (l *LeakyBucketLimiter) State(ctx context.Context) (LimiterState, error) {
	levelStr, err := l.client.Get(ctx, l.bucketKey()).Result()
	if errors.Is(err, redis.Nil) {
		// 桶从未使用过，视为初始状态：水位0
		now := time.Now().UnixMilli()
		return LimiterState{
			Level:             0,
			Remaining:         l.Capacity,
			Capacity:          l.Capacity,
			Rate:              l.LeakRate,
			LastUpdated:       now,
			NextAvailableTime: now,
			Type:              "leaky_bucket",
			Key:               l.Key,
		}, nil
	} else if err != nil {
		return LimiterState{}, err
	}

	tsStr, err := l.client.Get(ctx, l.tsKey()).Result()
	if errors.Is(err, redis.Nil) {
		// 状态不完整，兜底为初始状态
		now := time.Now().UnixMilli()
		return LimiterState{
			Level:             0,
			Remaining:         l.Capacity,
			Capacity:          l.Capacity,
			Rate:              l.LeakRate,
			LastUpdated:       now,
			NextAvailableTime: now,
			Type:              "leaky_bucket",
			Key:               l.Key,
		}, nil
	} else if err != nil {
		return LimiterState{}, err
	}

	level, err := strconv.ParseFloat(levelStr, 64)
	if err != nil {
		return LimiterState{}, fmt.Errorf("leaky bucket: invalid level value: %v", err)
	}

	lastTs, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return LimiterState{}, fmt.Errorf("leaky bucket: invalid ts value: %v", err)
	}

	now := time.Now()
	nowMs := now.UnixNano() / 1e6
	deltaMs := float64(nowMs - lastTs)
	if deltaMs < 0 {
		deltaMs = 0
	}

	// 在本地模拟一次泄漏，得到“当前真实水位”
	leak := (deltaMs * l.LeakRate) / 1000
	realLevel := level - leak
	if realLevel < 0 {
		realLevel = 0
	}

	remaining := l.Capacity - realLevel
	if remaining < 0 {
		remaining = 0
	}

	// 计算下一次可放行时间：
	// 若 realLevel < Capacity，则现在就能放行；
	// 若 realLevel >= Capacity，则需要等到 realLevel - Capacity 泄掉为止。
	var next time.Time
	if realLevel < l.Capacity {
		next = now
	} else {
		needLeak := realLevel - l.Capacity
		// needLeak / leakRate 得到需要的秒数
		waitSec := needLeak / l.LeakRate
		if waitSec < 0 {
			waitSec = 0
		}
		next = now.Add(time.Duration(waitSec * float64(time.Second)))
	}

	return LimiterState{
		Level:             realLevel,
		Remaining:         remaining,
		Capacity:          l.Capacity,
		Rate:              l.LeakRate,
		LastUpdated:       lastTs,
		NextAvailableTime: next.UnixMilli(),
		Type:              "leaky_bucket",
		Key:               l.Key,
	}, nil
}
