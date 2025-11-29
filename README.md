# go-redis-limiter

高性能、可扩展、Redis 驱动的分布式限流组件。
支持三大核心算法：

* **令牌桶（Token Bucket）**：高并发、支持突发流量、最适合 API QPS 限流
* **滑动窗口（Sliding Window Log）**：精确统计窗口内调用次数，适用于风控类限流
* **漏桶（Leaky Bucket）**：平滑流量整形，严格匀速输出

提供：

* **单桶（Single）限流器**
* **分片（Sharded）限流器**：突破单 Redis key 热点瓶颈，支持水平扩容
* **Redis Cluster 原生支持（hash tag）**
* **Option 模式参数传递（与不同限流器不冲突）**
* **Lua 脚本原子性保证**
* **毫秒级精度**
* **可观测性（State() 状态查询）**
* **redismock 单元测试友好**

适用于：

* API Gateway 全局限流
* 用户级/租户级限流
* 登录/验证码/发短信频控
* 消息/任务系统消费速率控制
* 内容生成/AI 请求限流
* 分片多租户场景的大规模限流管理

---

# Features

### 核心能力

| 算法             | 支持单桶 | 支持分片 | 精度 | 特点                 |
|----------------|------|------|----|--------------------|
| Token Bucket   | ✔    | ✔    | 毫秒 | 支持突发，高并发 API 最佳选择  |
| Sliding Window | ✔    | ✔    | 毫秒 | 精确限流，不受固定窗口边界影响    |
| Leaky Bucket   | ✔    | ✔    | 毫秒 | 匀速输出，严格整形，适合后台消费场景 |

### 工程特性

* Redis + Lua 原子化，无 race 条件
* Redis Cluster 兼容
* 分片（Sharded）可线性提升吞吐
* Option 模式配置，不污染不同限流器的命名空间
* redismock 友好，提供脚本 SHA 导出
* 完全 Go Module 化

---

# 安装

```bash
go get github.com/lifei6671/go-redis-limiter
```

---

# 快速开始

## 创建 Redis 客户端

```go
rdb := redis.NewClient(&redis.Options{
Addr: "127.0.0.1:6379",
})
```

---

# 令牌桶（Token Bucket）

## 创建一个单桶令牌桶

特点：

* 支持突发流量
* 高性能 API 限流最佳选择
* 每秒生成 Rate 个令牌，最大 Capacity

```go
tb := limiter.NewTokenBucketLimiter(
rdb,
"api:/v1/login",
limiter.WithTokenBucketRate(200), // 每秒生成 200 token
limiter.WithTokenBucketCapacity(200), // 桶容量 200
limiter.WithTokenBucketTTL(3*time.Second),
)
```

### 判断是否允许通过

```go
ok, err := tb.Allow(ctx)
if !ok {
// 限流
}
```

### 批量请求（N 个 token）

```go
ok, err := tb.AllowN(ctx, 5)
```

### 阻塞直到有令牌

```go
err := tb.Wait(ctx)
```

### 查询当前状态

```go
s, _ := tb.State(ctx)
fmt.Println("tokens:", s.Level)
```

---

# 分片令牌桶（Sharded Token Bucket）

适用于：

* 用户级限流（按 userID）
* IP 限流
* 避免 Redis 单 key 热点

分片策略：
Rate / Capacity 会被自动均分到每个 shard。

## 创建分片限流器

```go
tb := limiter.NewShardedTokenBucketLimiter(
rdb,
"api:/v1/chat",
32, // 32 个 shard
limiter.WithTokenBucketRate(10000),
limiter.WithTokenBucketCapacity(20000),
)
```

## 使用时必须传入 shard key

```go
userID := "user:123"

ok, err := tb.Allow(ctx, userID)
```

---

# 滑动窗口（Sliding Window Log）

特点：

* 精确统计最近 Window 时间内的所有请求
* 无固定窗口边界抖动
* 非常适用于登录风控、验证码、短信发送限制

## 创建单桶滑动窗口

```go
sw := limiter.NewSlidingWindowLimiter(
rdb,
"login:ip:1.2.3.4",
limiter.WithSlidingWindowWindow(1*time.Minute),
limiter.WithSlidingWindowLimit(10), // 1 分钟内最多 10 次
)
```

### 判断是否允许

```go
ok, err := sw.Allow(ctx)
```

---

# 分片滑动窗口（Sharded Sliding Window）

```go
ssw := limiter.NewShardedSlidingWindowLimiter(
rdb,
"api:/v1/verify",
16,
limiter.WithSlidingWindowWindow(1*time.Minute),
limiter.WithSlidingWindowLimit(1000),
)
```

使用：

```go
ok, err := ssw.Allow(ctx, "user:123")
```

---

# 漏桶（Leaky Bucket）

特点：

* 严格控制平均输出速率（平滑流量整形）
* 适合后台任务消费、消息队列读端限速
* 能储存的突发 = Capacity

## 创建单桶漏桶

```go
lb := limiter.NewLeakyBucketLimiter(
rdb,
"queue:video",
limiter.WithLeakyBucketRate(200), // 每秒处理 200
limiter.WithLeakyBucketCapacity(500), // 最大积压 500
)
```

判断：

```go
ok, err := lb.Allow(ctx)
```

---

# 分片漏桶（Sharded Leaky Bucket）

```go
slb := limiter.NewShardedLeakyBucketLimiter(
rdb,
"job:pay",
32,
limiter.WithLeakyBucketRate(1000),
limiter.WithLeakyBucketCapacity(3000),
)
```

使用：

```go
ok, err := slb.Allow(ctx, "user:42")
```

---

# 状态查询（State）

所有限流器都有：

```go
state, _ := limiter.State(ctx)
/*
state:
{
  Level             float64  // 当前水位
  Remaining         float64
  Capacity          float64
  Rate              float64
  LastUpdated       int64
  NextAvailableTime int64
  Type              string
  Key               string
}
*/
```

---

# Redis Cluster 支持

所有 key 使用模式：

```
prefix:{businessKey}:XX
```

hash tag `{businessKey}` 保证：

* tokens / ts / log / seq 落在同 slot
* Lua 脚本原子执行有效
* 分片 key 也正确路由

---

# Option 模式说明

每种限流器 Option 都前缀化，避免命名冲突：

```
WithTokenBucketRate
WithTokenBucketCapacity
WithTokenBucketTTL

WithSlidingWindowWindow
WithSlidingWindowLimit
WithSlidingWindowPrefix

WithLeakyBucketRate
WithLeakyBucketCapacity
```

所有限流器支持 With*Custom(fn)，用于分片扩展。

---

# 单元测试（redismock）

示例：

```go
mock.ExpectEvalSha(
limiter.TokenBucketScriptHash(),
[]string{"tbucket:{test}:tokens", "tbucket:{test}:ts"},
nowMs, 100.0, 100.0, 1.0, 2000,
).SetVal(int64(1))
```

你可以获取脚本 SHA：

```go
limiter.TokenBucketScriptHash()
limiter.SlidingWindowScriptHash()
limiter.LeakyBucketScriptHash()
```

---

# 性能说明

在常规 Redis 部署中：

* 单桶 TokenBucket：可达 10~20 万 QPS
* 分片 TokenBucket（32 shards）：线性扩展，可上 50~60 万 QPS
* 滑动窗口受 ZSET 成本影响，约 3~10 万 QPS
* 漏桶性能介于 TokenBucket 与 SlidingWindow 之间

---

# 适用场景对比

| 场景             | 推荐限流器                         | 理由           |
|----------------|-------------------------------|--------------|
| 高并发 API QPS 限制 | Token Bucket                  | 支持突发，高吞吐     |
| 登录错误、短信限制      | Sliding Window                | 精确窗口统计       |
| 任务系统消费速率       | Leaky Bucket                  | 匀速处理         |
| 用户级或租户级限流      | Sharded TokenBucket           | 分片避免热点       |
| 内容生成 / AI 请求   | Token Bucket + Sliding Window | QPS + 风控双层保护 |

---

# License

MIT License.
