package limiter

import "time"

// LeakyBucketOption 为漏桶限流器的配置项。
// 所有函数名均以 LeakyBucket 前缀开头，避免与其它限流器的 Option 冲突。
type LeakyBucketOption func(*LeakyBucketLimiter)

// WithLeakyBucketRate 设置泄漏速率（单位/秒）。
// 例如：leakRate = 100 表示每秒最多漏出100个请求（即平滑速率）。
func WithLeakyBucketRate(leakRate float64) LeakyBucketOption {
	return func(l *LeakyBucketLimiter) {
		if leakRate <= 0 {
			panic("leaky bucket: leakRate must > 0")
		}
		l.LeakRate = leakRate
	}
}

// WithLeakyBucketCapacity 设置桶容量（允许堆积的最大请求数）。
func WithLeakyBucketCapacity(cap float64) LeakyBucketOption {
	return func(l *LeakyBucketLimiter) {
		if cap <= 0 {
			panic("leaky bucket: capacity must > 0")
		}
		l.Capacity = cap
	}
}

// WithLeakyBucketTTL 设置 Redis key TTL。
func WithLeakyBucketTTL(ttl time.Duration) LeakyBucketOption {
	return func(l *LeakyBucketLimiter) {
		if ttl > 0 {
			l.TTL = ttl
		}
	}
}

// WithLeakyBucketPrefix 设置 Redis key 前缀。
func WithLeakyBucketPrefix(prefix string) LeakyBucketOption {
	return func(l *LeakyBucketLimiter) {
		if prefix != "" {
			l.Prefix = prefix
		}
	}
}

// WithLeakyBucketCustom 提供一个扩展入口，方便外部自定义更复杂的初始化逻辑。
// 例如在分片实现里对 LeakRate/Capacity 做缩放。
func WithLeakyBucketCustom(fn func(*LeakyBucketLimiter)) LeakyBucketOption {
	return func(l *LeakyBucketLimiter) {
		fn(l)
	}
}
