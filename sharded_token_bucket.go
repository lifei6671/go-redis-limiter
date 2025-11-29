package limiter

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/go-redis/redis/v8"
)

// ShardedTokenBucketLimiter 是“分片令牌桶”实现。
// 通过将“全局限流”拆成多个子桶（shard），可以线性提升吞吐能力，避免单 key 热点。
// 每个 shard 是一个独立的 TokenBucketLimiter。
//
// 典型用法：
//   - 针对某一类业务 key（例如 "api:/v1/chat"），
//   - 按 userID / IP / tenantID 做 shardKey 路由，
//   - 每个 shard 使用全局 Rate/Capacity 的 1/N。
type ShardedTokenBucketLimiter struct {
	shards []*TokenBucketLimiter
	count  int
}

// NewShardedTokenBucketLimiter 创建一个分片令牌桶。
//   - client: Redis 客户端
//   - key:    全局业务 key（例如 "api:/v1/chat"）
//   - shardCount: 分片数量（默认为16，如果传 <=0 则强制为16）
//   - opts:   令牌桶配置（全局 Rate/Capacity/TTL/Prefix 等）
//     注意：Rate 和 Capacity 会在内部按 shardCount 均分到每个 shard 上。
func NewShardedTokenBucketLimiter(
	client *redis.Client,
	key string,
	shardCount int,
	opts ...TokenBucketOption,
) *ShardedTokenBucketLimiter {

	if client == nil {
		panic("sharded token bucket: redis client is nil")
	}
	if key == "" {
		panic("sharded token bucket: key is empty")
	}
	if shardCount <= 0 {
		shardCount = 16
	}

	shards := make([]*TokenBucketLimiter, shardCount)

	for i := 0; i < shardCount; i++ {
		shardKey := fmt.Sprintf("%s:shard:%d", key, i)

		// 拷贝一份 opts，避免对原切片产生副作用
		innerOpts := append([]TokenBucketOption{}, opts...)

		// 使用 Custom Option 在每个 shard 上缩放 rate/capacity
		innerOpts = append(innerOpts, WithTokenBucketCustom(func(tb *TokenBucketLimiter) {
			tb.Rate = tb.Rate / float64(shardCount)
			if tb.Rate <= 0 {
				tb.Rate = 1
			}
			tb.Capacity = tb.Capacity / float64(shardCount)
			if tb.Capacity <= 0 {
				tb.Capacity = 1
			}
		}))

		shards[i] = NewTokenBucketLimiter(client, shardKey, innerOpts...)
	}

	return &ShardedTokenBucketLimiter{
		shards: shards,
		count:  shardCount,
	}
}

// pick 根据 shardKey 选择某一个 shard。
// 使用 FNV-1a 哈希，简单且分布较均匀。
func (s *ShardedTokenBucketLimiter) pick(shardKey string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(shardKey))
	return int(h.Sum32()) % s.count
}

// Allow 对指定 shardKey 尝试获取 1 个 token。
// 常见用法：shardedLimiter.Allow(ctx, userID)
func (s *ShardedTokenBucketLimiter) Allow(ctx context.Context, shardKey string) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].Allow(ctx)
}

// AllowN 对指定 shardKey 尝试获取 n 个 token。
func (s *ShardedTokenBucketLimiter) AllowN(ctx context.Context, shardKey string, n int64) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].AllowN(ctx, n)
}

// Wait 对指定 shardKey 阻塞直到获取到一个 token 或 ctx 超时。
func (s *ShardedTokenBucketLimiter) Wait(ctx context.Context, shardKey string, maxWait time.Duration) error {
	idx := s.pick(shardKey)
	return s.shards[idx].Wait(ctx, maxWait)
}

// State 返回某个 shardKey 对应的 shard 的状态。
// 注意：这不是“全局聚合状态”，而是“该 shard 的局部状态”。
func (s *ShardedTokenBucketLimiter) State(ctx context.Context, shardKey string) (LimiterState, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].State(ctx)
}
