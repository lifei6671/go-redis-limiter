package limiter

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/go-redis/redis/v8"
)

// ShardedLeakyBucketLimiter 是“分片版”的漏桶限流器。
// 通过多个 LeakyBucketLimiter 分摊压力，提升吞吐能力。
// 使用 shardKey 做路由（例如 userID、IP、tenantID）。
type ShardedLeakyBucketLimiter struct {
	shards []*LeakyBucketLimiter
	count  int
}

// NewShardedLeakyBucketLimiter 创建一个分片漏桶限流器。
// 说明：
//   - key 是全局逻辑 key，实际每个 shard 会在 key 后增加 ":shard:i"
//   - shardCount 为分片数量，会直接影响单 shard 的 LeakRate/Capacity
//   - opts 为基础 LeakyBucket 配置（LeakRate/Capacity/TTL/Prefix等）
//     然后内部会将 LeakRate 和 Capacity 均摊到各 shard 上。
func NewShardedLeakyBucketLimiter(
	client *redis.Client,
	key string,
	shardCount int,
	opts ...LeakyBucketOption,
) *ShardedLeakyBucketLimiter {

	if client == nil {
		panic("sharded leaky bucket: redis client is nil")
	}
	if key == "" {
		panic("sharded leaky bucket: key is empty")
	}
	if shardCount <= 0 {
		shardCount = 16
	}

	shards := make([]*LeakyBucketLimiter, shardCount)

	for i := 0; i < shardCount; i++ {
		shardKey := fmt.Sprintf("%s:shard:%d", key, i)

		// 复制外部的 opts，避免多 shard 间共享同一个 slice 产生副作用
		innerOpts := append([]LeakyBucketOption{}, opts...)

		// 通过 Custom Option，在每个 shard 上执行“均摊速率与容量”的逻辑。
		innerOpts = append(innerOpts, WithLeakyBucketCustom(func(l *LeakyBucketLimiter) {
			// 按 shardCount 均分 LeakRate 和 Capacity
			l.LeakRate = l.LeakRate / float64(shardCount)
			if l.LeakRate <= 0 {
				l.LeakRate = 1 // 最小保护值
			}
			l.Capacity = l.Capacity / float64(shardCount)
			if l.Capacity <= 0 {
				l.Capacity = 1
			}
		}))

		shards[i] = NewLeakyBucketLimiter(client, shardKey, innerOpts...)
	}

	return &ShardedLeakyBucketLimiter{
		shards: shards,
		count:  shardCount,
	}
}

// pick 根据 shardKey 计算落在哪个 shard 上。
// 使用 FNV-1a 哈希，简单且足够均匀。
func (s *ShardedLeakyBucketLimiter) pick(shardKey string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(shardKey))
	return int(h.Sum32()) % s.count
}

// Allow 尝试对指定 shardKey 获取一个许可。
// 典型用法：shardedLimiter.Allow(ctx, userID)
func (s *ShardedLeakyBucketLimiter) Allow(ctx context.Context, shardKey string) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].Allow(ctx)
}

// AllowN 尝试对指定 shardKey 获取 n 个许可。
func (s *ShardedLeakyBucketLimiter) AllowN(ctx context.Context, shardKey string, n int64) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].AllowN(ctx, n)
}

// Wait 阻塞直到 shardKey 对应的漏桶中腾出空间。
func (s *ShardedLeakyBucketLimiter) Wait(ctx context.Context, shardKey string, maxWait time.Duration) error {
	idx := s.pick(shardKey)
	return s.shards[idx].Wait(ctx, maxWait)
}

// State 返回 shardKey 所在分片的状态。
// 注意：这是“某一个 shard 的状态”，而不是全局聚合结果。
func (s *ShardedLeakyBucketLimiter) State(ctx context.Context, shardKey string) (LimiterState, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].State(ctx)
}
