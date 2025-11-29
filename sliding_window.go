package limiter

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// SingleSlidingWindowLimiter 实现“单桶滑动窗口”限流器。
// 特点：
//   - 使用 ZSET 存储请求时间戳，实现真正“滑动”的窗口统计
//   - 与固定窗口相比，边界更加平滑
//   - 适合短信/验证码/登录错误等对“最近 N 秒调用次数”有要求的场景
type SingleSlidingWindowLimiter struct {
	client *redis.Client

	Key    string        // 业务 key
	Prefix string        // Redis key 前缀，默认 "sw"
	Window time.Duration // 窗口大小，例如 1 * time.Minute
	Limit  int64         // 窗口内最大允许请求数
	TTL    time.Duration // key 过期时间，建议 >= Window * 2
}

// NewSlidingWindowLimiter 创建一个单桶滑动窗口限流器。
func NewSlidingWindowLimiter(
	client *redis.Client,
	key string,
	opts ...SlidingWindowOption,
) *SingleSlidingWindowLimiter {

	if client == nil {
		panic("sliding window: redis client is nil")
	}
	if key == "" {
		panic("sliding window: key is empty")
	}

	l := &SingleSlidingWindowLimiter{
		client: client,
		Key:    key,
		Prefix: "sw",
		Window: 1 * time.Minute,
		Limit:  60,
		TTL:    2 * time.Minute,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// logKey 返回 ZSET：存储请求时间戳的 key。
func (l *SingleSlidingWindowLimiter) logKey() string {
	return fmt.Sprintf("%s:{%s}:log", l.Prefix, l.Key)
}

// seqKey 返回自增序列 key，保证 ZSET member 唯一。
func (l *SingleSlidingWindowLimiter) seqKey() string {
	return fmt.Sprintf("%s:{%s}:seq", l.Prefix, l.Key)
}

// Allow 尝试为当前请求在滑动窗口中占一个名额。
func (l *SingleSlidingWindowLimiter) Allow(ctx context.Context) (bool, error) {
	return l.AllowN(ctx, 1)
}

// AllowN 尝试一次通过 n 个请求。
// 精确滑动窗口场景下，一般 n=1；如果有 n>1 的需求，可以扩展脚本一次写入多个 member。
// 这里为了简化与保持原子性，不支持 n>1。
func (l *SingleSlidingWindowLimiter) AllowN(ctx context.Context, n int64) (bool, error) {
	if n != 1 {
		return false, fmt.Errorf("sliding window: AllowN only supports n=1 for now")
	}

	nowMs := float64(time.Now().UnixNano() / 1e6)
	windowMs := l.Window.Milliseconds()
	ttlMs := l.TTL.Milliseconds()

	res, err := slidingWindowScript.Run(
		ctx,
		l.client,
		[]string{l.logKey(), l.seqKey()},
		nowMs,
		windowMs,
		l.Limit,
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
		return false, fmt.Errorf("sliding window: unexpected script result: %#v", res)
	}
}

// Wait 简单实现一个轮询等待：
//   - 如果 Allow 返回 false，则 sleep 一段时间再重试。
//   - 直到通过或 ctx 超时。
func (l *SingleSlidingWindowLimiter) Wait(ctx context.Context, maxWait time.Duration) error {
	maxWait = max(maxWait, 0)
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

// State 返回当前滑动窗口内的请求数量等状态。
func (l *SingleSlidingWindowLimiter) State(ctx context.Context) (LimiterState, error) {
	now := float64(time.Now().UnixNano() / 1e6)
	windowMs := l.Window.Milliseconds()
	minScore := now - float64(windowMs)

	// 统计 [minScore, +inf] 范围内的元素数量，即当前窗口内请求数。
	card, err := l.client.ZCount(ctx, l.logKey(), fmt.Sprintf("%f", minScore), "+inf").Result()
	if err != nil {
		return LimiterState{}, err
	}

	level := float64(card)
	remaining := float64(l.Limit) - level
	if remaining < 0 {
		remaining = 0
	}

	rate := float64(l.Limit) / l.Window.Seconds()

	nowMsInt := time.Now().UnixMilli()

	return LimiterState{
		Level:             level,
		Remaining:         remaining,
		Capacity:          float64(l.Limit),
		Rate:              rate,
		LastUpdated:       nowMsInt,
		NextAvailableTime: nowMsInt, // 精确下一次可用时间可按需要进一步计算
		Type:              "sliding_window",
		Key:               l.Key,
	}, nil
}
