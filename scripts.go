package limiter

import "github.com/go-redis/redis/v8"

// tokenBucketScript 使用 Redis + Lua 实现原子化令牌桶逻辑：
//   - 支持毫秒级 refill
//   - 令牌数不会超过 Capacity
//   - 不足时拒绝并不修改状态
//
// KEYS[1] = tokensKey（当前 token 数，浮点数）
// KEYS[2] = tsKey    （上次更新时间，毫秒时间戳）
//
// ARGV[1] = nowMs    （当前时间，毫秒）
// ARGV[2] = rate     （生成速率，token/sec）
// ARGV[3] = capacity （桶容量）
// ARGV[4] = req      （本次请求需要的 token 数，通常为 1）
// ARGV[5] = ttlMs    （key 过期时间，毫秒，用于清理闲置 key）
var tokenBucketScript = redis.NewScript(`
local tokensKey = KEYS[1]
local tsKey     = KEYS[2]

local now      = tonumber(ARGV[1])
local rate     = tonumber(ARGV[2])
local capacity = tonumber(ARGV[3])
local req      = tonumber(ARGV[4])
local ttl      = tonumber(ARGV[5])

-- 当前 token 数（第一次使用则默认为满桶）
local tokens = tonumber(redis.call("GET", tokensKey)) or capacity
-- 上次更新时间（第一次使用则认为“当前时间”）
local lastTs = tonumber(redis.call("GET", tsKey)) or now

-- 计算从 lastTs 到 now 的时间差（毫秒）
local delta = now - lastTs
if delta < 0 then
  delta = 0
end

-- 根据时间差进行 refill：newTokens = rate * delta / 1000
local refill = (delta * rate) / 1000
tokens = tokens + refill
if tokens > capacity then
  tokens = capacity
end

-- 判断是否有足够的令牌
if tokens < req then
  return 0
end

-- 消耗令牌
tokens = tokens - req

-- 回写最新 token 数及时间戳，并设置 TTL
redis.call("SET", tokensKey, tokens, "PX", ttl)
redis.call("SET", tsKey, now, "PX", ttl)

return 1
`)

// leakyBucketScript 实现“漏桶”算法的核心逻辑，保证在 Redis 端原子执行。
// 算法：
//
//	level(t) = max(0, level(last) - leakRate * (now - last)/1000)
//	if level(t) + req > capacity -> 拒绝
//	else level(t) = level(t) + req -> 允许
//
// KEYS[1] = bucket level key (string，存当前水位，浮点数)
// KEYS[2] = ts key          (string，存上次更新时间，毫秒时间戳)
//
// ARGV[1] = nowMs      (当前时间，毫秒)
// ARGV[2] = leakRate   (泄漏速率，单位：单位/秒)
// ARGV[3] = capacity   (桶容量，最大水位)
// ARGV[4] = reqTokens  (本次请求消耗多少单位，一般为1)
// ARGV[5] = ttlMs      (key 过期时间，毫秒)
var leakyBucketScript = redis.NewScript(`
local bucketKey = KEYS[1]
local tsKey     = KEYS[2]

local now       = tonumber(ARGV[1])
local leakRate  = tonumber(ARGV[2])
local capacity  = tonumber(ARGV[3])
local req       = tonumber(ARGV[4])
local ttl       = tonumber(ARGV[5])

-- 当前水位（如果不存在，则视为0）
local level = tonumber(redis.call("GET", bucketKey)) or 0
-- 上次更新时间（如果不存在，则视为当前时间）
local lastTs = tonumber(redis.call("GET", tsKey)) or now

-- 计算时间差，单位毫秒
local delta = now - lastTs
if delta < 0 then
  delta = 0
end

-- 按时间差计算应泄漏的水量：leak = leakRate * delta / 1000
local leak = (delta * leakRate) / 1000
level = level - leak
if level < 0 then
  level = 0
end

-- 判断本次请求能否放入桶中
if level + req > capacity then
  -- 超出容量，拒绝
  return 0
end

-- 接受本次请求：增加水位
level = level + req

-- 写回 Redis，并设置 TTL，防止 key 永久存在
redis.call("SET", bucketKey, level, "PX", ttl)
redis.call("SET", tsKey, now, "PX", ttl)

return 1
`)

// slidingWindowScript 使用 ZSET + Lua 实现“精确滑动窗口”限流。
// 算法：
//   - 每次请求：
//     1) 删除窗口外的记录：ZREMRANGEBYSCORE key 0 (now-window)
//     2) 统计窗口内记录数：count = ZCARD
//     3) 若 count >= limit -> 拒绝
//     4) 否则：ZADD now member，并设置 TTL
//
// KEYS[1] = logKey (ZSET，用于存储请求时间戳)
// KEYS[2] = seqKey (String，自增序列，保证 member 唯一)
//
// ARGV[1] = nowMs    (当前时间，毫秒)
// ARGV[2] = windowMs (窗口大小，毫秒)
// ARGV[3] = limit    (窗口内最大允许请求数)
// ARGV[4] = ttlMs    (key 过期时间，毫秒)
var slidingWindowScript = redis.NewScript(`
local logKey = KEYS[1]
local seqKey = KEYS[2]

local now    = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit  = tonumber(ARGV[3])
local ttl    = tonumber(ARGV[4])

local minScore = now - window

-- 删除窗口之外的旧记录
redis.call("ZREMRANGEBYSCORE", logKey, 0, minScore)

-- 窗口内当前请求数量
local count = redis.call("ZCARD", logKey)
if count >= limit then
  return 0
end

-- 为本次请求生成唯一 member
local seq = redis.call("INCR", seqKey)
local member = now .. "-" .. seq

-- 写入本次请求
redis.call("ZADD", logKey, now, member)

-- 设置 TTL，避免 key 泄漏
redis.call("PEXPIRE", logKey, ttl)
redis.call("PEXPIRE", seqKey, ttl)

return 1
`)
