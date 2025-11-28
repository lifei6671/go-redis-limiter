package limiter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

const simpleRedisRemainderKeyPrefix = "simple:remainder"
const defaultRedisLimiterKeyExpire = 2 * time.Second // 仅需2倍窗口即可

type Remainder interface {
	Push(ctx context.Context, count int64) error
	Available(ctx context.Context) error
}

type SimpleRedisRemainder struct {
	client    *redis.Client
	Window    int64  // 窗口大小(秒)
	Count     int64  // 允许次数
	Key       string // 限流键
	KeyPrefix string // 前缀
}

// NewSimpleRedisRemainder 创建限流器
func NewSimpleRedisRemainder(client *redis.Client, window int64, count int64, key string) Remainder {
	return &SimpleRedisRemainder{
		client:    client,
		Window:    window,
		Count:     count,
		Key:       key,
		KeyPrefix: simpleRedisRemainderKeyPrefix,
	}
}

// buildKey 生成固定窗口 key
func (s *SimpleRedisRemainder) buildKey() string {
	windowIndex := time.Now().Unix() / s.Window
	// hash-tag pattern: {key} 确保 Redis Cluster 保持 slot 一致
	return fmt.Sprintf("%s:{%s}:%d", s.KeyPrefix, s.Key, windowIndex)
}

// Push 增加窗口内计数
func (s *SimpleRedisRemainder) Push(ctx context.Context, count int64) error {
	key := s.buildKey()

	pipe := s.client.TxPipeline()
	incr := pipe.IncrBy(ctx, key, count)
	pipe.Expire(ctx, key, defaultRedisLimiterKeyExpire)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}

	if incr.Val() > s.Count {
		return ErrLimiter
	}
	return nil
}

// Available 判断是否还有余量，不修改计数
func (s *SimpleRedisRemainder) Available(ctx context.Context) error {
	key := s.buildKey()

	// 只读，不 incr
	count, err := s.client.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return nil // key 不存在，无消耗
	} else if err != nil {
		return err
	}

	if count >= s.Count {
		return ErrLimiter
	}

	// 主动设置过期避免遗留 key
	s.client.Expire(ctx, key, defaultRedisLimiterKeyExpire)

	return nil
}

var _ Remainder = (*SimpleRedisRemainder)(nil)
