package limiter

import (
	"time"

	"github.com/go-redis/redis/v8"
)

var _ Factory = (*limitConfig)(nil)

// Factory 限流器工厂接口
type Factory interface {
	// Create 创建限制器
	Create(key string, r *redis.Client) Limiter
}

type limitConfig struct {
	LimitItem []*LimitOption
}

// LimitOption 限制每个key在每个duration内最多请求count次 , 超过timeout直接返回错误
type LimitOption struct {
	Enable bool `yaml:"enable"`
	// Key 按照Key做限流
	Key string `yaml:"key"`
	// Count 数量
	Count int64 `yaml:"count"`
	// Duration 区间
	Duration time.Duration `yaml:"duration"`
	// Timeout 超时时间
	Timeout time.Duration `yaml:"timeout"`
}

func (l *limitConfig) Create(key string, r *redis.Client) Limiter {
	if l == nil {
		return NewNopLimiter()
	}
	for _, v := range l.LimitItem {
		if v.Key != key {
			continue
		}
		if !v.Enable {
			continue
		}
		bucket := NewShardedRedisTokenBucket(r, v.Key, float64(v.Count)/v.Duration.Seconds(), v.Count, 1, v.Duration*10)
		return NewWrapperLimiter(bucket, v.Key, v.Timeout)
	}
	return NewNopLimiter()
}

func New(option *LimitOption) Factory {
	return &limitConfig{
		LimitItem: []*LimitOption{option},
	}
}
