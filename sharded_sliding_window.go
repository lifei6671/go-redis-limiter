package limiter

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/go-redis/redis/v8"
)

// ShardedSlidingWindowLimiter 是“分片滑动窗口”限流器。
// 将一个全局限流拆成多个滑动窗口 shard，使用 shardKey 路由请求。
// 典型场景：针对某个 API，按用户 ID/IP 分 shard 做限流，避免单 key 热点。
type ShardedSlidingWindowLimiter struct {
	shards []*SingleSlidingWindowLimiter
	count  int
}

// NewShardedSlidingWindowLimiter 创建一个分片滑动窗口限流器。
//   - client: Redis 客户端
//   - key:    全局业务 key，例如 "api:/v1/chat"
//   - shardCount: 分片数量，传 <=0 默认使用 16
//   - opts:   滑动窗口参数（Window/Limit/TTL/Prefix 等）
//     注意：Limit 会在内部按 shardCount 均分。
func NewShardedSlidingWindowLimiter(
	client *redis.Client,
	key string,
	shardCount int,
	opts ...SlidingWindowOption,
) *ShardedSlidingWindowLimiter {

	if client == nil {
		panic("sharded sliding window: redis client is nil")
	}
	if key == "" {
		panic("sharded sliding window: key is empty")
	}
	if shardCount <= 0 {
		shardCount = 16
	}

	shards := make([]*SingleSlidingWindowLimiter, shardCount)

	for i := 0; i < shardCount; i++ {
		shardKey := fmt.Sprintf("%s:shard:%d", key, i)

		innerOpts := append([]SlidingWindowOption{}, opts...)

		// 通过 Custom Option 在每个 shard 上均摊 Limit。
		innerOpts = append(innerOpts, WithSlidingWindowCustom(func(l *SingleSlidingWindowLimiter) {
			l.Limit = l.Limit / int64(shardCount)
			if l.Limit <= 0 {
				l.Limit = 1
			}
		}))

		shards[i] = NewSlidingWindowLimiter(client, shardKey, innerOpts...)
	}

	return &ShardedSlidingWindowLimiter{
		shards: shards,
		count:  shardCount,
	}
}

// pick 根据 shardKey 选择一个 shard。
func (s *ShardedSlidingWindowLimiter) pick(shardKey string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(shardKey))
	return int(h.Sum32()) % s.count
}

// Allow 对指定 shardKey 尝试通过一个请求。
func (s *ShardedSlidingWindowLimiter) Allow(ctx context.Context, shardKey string) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].Allow(ctx)
}

// AllowN 对指定 shardKey 尝试通过 n 个请求。
func (s *ShardedSlidingWindowLimiter) AllowN(ctx context.Context, shardKey string, n int64) (bool, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].AllowN(ctx, n)
}

// Wait 对指定 shardKey 阻塞直到窗口中有空间，或 ctx 超时。
func (s *ShardedSlidingWindowLimiter) Wait(ctx context.Context, shardKey string, maxWait time.Duration) error {
	idx := s.pick(shardKey)
	return s.shards[idx].Wait(ctx, maxWait)
}

// State 返回 shardKey 对应分片的状态。
func (s *ShardedSlidingWindowLimiter) State(ctx context.Context, shardKey string) (LimiterState, error) {
	idx := s.pick(shardKey)
	return s.shards[idx].State(ctx)
}
