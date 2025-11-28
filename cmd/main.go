package main

import (
	"context"
	"log"
	"time"

	"github.com/go-redis/redis/v8"

	limiter "github.com/lifei6671/go-redis-limiter"
)

func main() {
	factory := limiter.New(&limiter.LimitOption{
		Enable:   true,
		Key:      "qpsLimit",
		Count:    2,
		Duration: time.Second * 5,
		Timeout:  time.Second,
	})
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "redis_5kYGjd",
	})
	redisLimiter := factory.Create("qpsLimit", client)

	for i := 0; i < 10; i++ {
		err := redisLimiter.Wait(context.Background())
		if err != nil {
			log.Fatalf("[%d]:limiter wait err:%s", i, err)
		}
		log.Printf("[%d]:limiter wait ok", i)
	}
}
