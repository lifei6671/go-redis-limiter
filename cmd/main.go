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
		Password: "redis_5kYGjd",
	})
	redisLimiter := limiter.NewShardedTokenBucketLimiter(client, "limiter:", 2,
		limiter.WithTokenBucketRate(2),
		limiter.WithTokenBucketCapacity(2),
	)

	for i := 0; i < 10; i++ {
		err := redisLimiter.Wait(context.Background(), "limiter:", time.Millisecond*100)
		if err != nil {
			log.Printf("[%d]:limiter wait err:%s", i, err)
		} else {
			log.Printf("[%d]:limiter wait ok", i)
		}
	}
}
