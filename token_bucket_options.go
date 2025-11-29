package limiter

import "time"

// TokenBucketOption 是单桶令牌桶的配置项。
// 所有函数名均以 TokenBucket 前缀开头，避免与其他限流算法的 Option 冲突。
type TokenBucketOption func(*TokenBucketLimiter)

// WithTokenBucketRate 设置令牌桶的生成速率（token/sec）。
func WithTokenBucketRate(rate float64) TokenBucketOption {
	return func(tb *TokenBucketLimiter) {
		if rate <= 0 {
			panic("token bucket: rate must > 0")
		}
		tb.Rate = rate
	}
}

// WithTokenBucketCapacity 设置令牌桶的容量。
func WithTokenBucketCapacity(cap float64) TokenBucketOption {
	return func(tb *TokenBucketLimiter) {
		if cap <= 0 {
			panic("token bucket: capacity must > 0")
		}
		tb.Capacity = cap
	}
}

// WithTokenBucketTTL 设置 Redis key 的 TTL。
func WithTokenBucketTTL(ttl time.Duration) TokenBucketOption {
	return func(tb *TokenBucketLimiter) {
		if ttl > 0 {
			tb.TTL = ttl
		}
	}
}

// WithTokenBucketPrefix 设置 Redis key 的前缀。
func WithTokenBucketPrefix(prefix string) TokenBucketOption {
	return func(tb *TokenBucketLimiter) {
		if prefix != "" {
			tb.Prefix = prefix
		}
	}
}

// WithTokenBucketCustom 提供一个自定义扩展入口。
// 适合在分片实现中对 Rate/Capacity 做缩放等操作。
func WithTokenBucketCustom(fn func(*TokenBucketLimiter)) TokenBucketOption {
	return func(tb *TokenBucketLimiter) {
		fn(tb)
	}
}
