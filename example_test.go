package limiter_test

import (
	"context"
	"log"
	"time"

	"github.com/go-redis/redis/v8"

	limiter "github.com/lifei6671/go-redis-limiter"
)

func ExampleNewShardedTokenBucketLimiter() {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "redis_password",
	})
	redisLimiter := limiter.NewShardedTokenBucketLimiter(client, "limiter:", 2,
		limiter.WithTokenBucketRate(2),
		limiter.WithTokenBucketCapacity(2),
	)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := redisLimiter.Wait(ctx, "limiter:", time.Millisecond*500)
		if err != nil {
			log.Printf("[%d]:limiter wait err:%s", i, err)
		} else {
			log.Printf("[%d]:limiter wait ok", i)
		}
		status, err := redisLimiter.State(ctx, "limiter:")
		log.Printf("[%d]:limiter:status:%s", i, status)
	}
}

func ExampleNewLeakyBucketLimiter() {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "redis_password",
	})
	redisLimiter := limiter.NewLeakyBucketLimiter(client, "limiter:",
		limiter.WithLeakyBucketCapacity(2),
		limiter.WithLeakyBucketRate(2),
	)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := redisLimiter.Wait(ctx, time.Millisecond*500)
		if err != nil {
			log.Printf("[%d]:limiter wait err:%s", i, err)
		} else {
			log.Printf("[%d]:limiter wait ok", i)
		}
		status, err := redisLimiter.State(ctx)
		log.Printf("[%d]:limiter:status:%s", i, status)
	}
}

func ExampleNewShardedSlidingWindowLimiter() {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "redis_password",
	})
	redisLimiter := limiter.NewShardedSlidingWindowLimiter(client, "limiter:", 2,
		limiter.WithSlidingWindowLimit(2),
		limiter.WithSlidingWindowTTL(time.Microsecond),
	)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := redisLimiter.Wait(ctx, "limiter:", time.Millisecond*500)
		if err != nil {
			log.Printf("[%d]:limiter wait err:%s", i, err)
		} else {
			log.Printf("[%d]:limiter wait ok", i)
		}
		status, err := redisLimiter.State(ctx, "limiter:")
		log.Printf("[%d]:limiter:status:%s", i, status)
	}
}
