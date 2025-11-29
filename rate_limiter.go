package limiter

import (
	"context"
	"time"
)

// RateLimiter 是所有单桶限流器的统一接口。
// 注意：分片型限流器通常会增加 shardKey 入参，所以一般不会直接实现这个接口，而是单桶实现。
type RateLimiter interface {
	// Allow 尝试获取一个许可（例如 1 个请求）。
	// 返回值：
	//   allowed = true  -> 允许通过
	//   allowed = false -> 被限流
	// error 通常表示后端（例如 Redis）异常，由上层决定是 fail-open 还是 fail-close。
	Allow(ctx context.Context) (bool, error)

	// AllowN 尝试一次性获取 n 个许可。
	// 对于令牌桶/漏桶非常有用（批量扣减），滑动窗口等不一定支持 n>1。
	AllowN(ctx context.Context, n int64) (bool, error)

	// Wait 阻塞直到成功获取 1 个许可，或者 ctx 超时/取消。
	// 适合节流场景，例如严格“匀速”处理任务队列。
	Wait(ctx context.Context, maxWait time.Duration) error

	// State 返回限流器当前状态，用于监控和调试。
	State(ctx context.Context) (LimiterState, error)
}

// LimiterState 为各类限流器提供了一个尽量通用的状态结构。
// 各字段的含义可能会因具体算法略有区别，但整体语义保持一致。
type LimiterState struct {
	// Level 当前“水位”：
	//  - 令牌桶：当前可用 token 数
	//  - 滑动窗口：当前窗口内请求数
	Level float64

	// Remaining 剩余空间：
	//  - 令牌桶：剩余 token 数（通常与 Level 相同）
	//  - 滑动窗口：Limit - 当前窗口内请求数
	Remaining float64

	// Capacity 最大容量：
	//  - 令牌桶：最大 token 数
	//  - 滑动窗口：窗口内允许的最大请求数
	Capacity float64

	// Rate 速率：
	//  - 令牌桶：token 生成速率（token/sec）
	//  - 滑动窗口：每秒平均允许请求数（Limit / Window）
	Rate float64

	// LastUpdated 上一次更新状态的时间戳（毫秒）。
	// 实现通常会用 Redis 里的 ts 字段，或者本地 now。
	LastUpdated int64

	// NextAvailableTime 当当前被限流时，理论上下一次可通过的时间（毫秒时间戳）。
	// 对滑动窗口/令牌桶/漏桶的计算方式各不相同。
	NextAvailableTime int64

	// Type 限流器类型（例如："token_bucket", "sliding_window"）
	Type string

	// Key 该限流器的业务 key（例如 "api:/v1/login"、"user:123"）
	Key string
}

// RateShardedLimiter 支持分片的限流器接口
type RateShardedLimiter interface {
	Allow(ctx context.Context, shardKey string) (bool, error)
	AllowN(ctx context.Context, shardKey string, n int64) (bool, error)
	State(ctx context.Context, shardKey string) (LimiterState, error)
	Wait(ctx context.Context, shardKey string, maxWait time.Duration) error
}
