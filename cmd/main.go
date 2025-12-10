package main

import (
	"context"
	"log"
	"time"

	"github.com/go-redis/redis/v8"

	limiter "github.com/lifei6671/go-redis-limiter"
)

func main() {

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
