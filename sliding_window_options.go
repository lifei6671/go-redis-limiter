package limiter

import "time"

// SlidingWindowOption 为滑动窗口限流器的配置项。
// 使用 SlidingWindow 前缀，避免与其他限流器的 Option 冲突。
type SlidingWindowOption func(*SingleSlidingWindowLimiter)

// WithSlidingWindowWindow 设置窗口大小。
func WithSlidingWindowWindow(d time.Duration) SlidingWindowOption {
	return func(l *SingleSlidingWindowLimiter) {
		if d > 0 {
			l.Window = d
		}
	}
}

// WithSlidingWindowLimit 设置窗口内允许的最大请求数。
func WithSlidingWindowLimit(limit int64) SlidingWindowOption {
	return func(l *SingleSlidingWindowLimiter) {
		if limit > 0 {
			l.Limit = limit
		}
	}
}

// WithSlidingWindowTTL 设置 Redis key 的 TTL。
func WithSlidingWindowTTL(ttl time.Duration) SlidingWindowOption {
	return func(l *SingleSlidingWindowLimiter) {
		if ttl > 0 {
			l.TTL = ttl
		}
	}
}

// WithSlidingWindowPrefix 设置 Redis key 前缀。
func WithSlidingWindowPrefix(prefix string) SlidingWindowOption {
	return func(l *SingleSlidingWindowLimiter) {
		if prefix != "" {
			l.Prefix = prefix
		}
	}
}

// WithSlidingWindowCustom 提供一个自定义扩展入口。
// 主要用于分片实现中对 Limit 等参数做缩放。
func WithSlidingWindowCustom(fn func(*SingleSlidingWindowLimiter)) SlidingWindowOption {
	return func(l *SingleSlidingWindowLimiter) {
		fn(l)
	}
}
