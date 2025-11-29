package limiter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

func TestShardedRedisTokenBucket_Allow(t *testing.T) {

	t.Run("allow limit ok", func(t *testing.T) {
		patches := gomonkey.ApplyMethodReturn(tokenBucketScript, "Run", redis.NewCmdResult(int64(1), nil))
		defer patches.Reset()

		rLimit := NewShardedRedisTokenBucket(nil, "test", 2.0, 10.0, 1, time.Minute)
		ok, err := rLimit.Allow(context.Background(), "key")

		assert.Nil(t, err)
		assert.True(t, ok)
	})

	t.Run("allow limit err", func(t *testing.T) {
		patches := gomonkey.ApplyMethodReturn(tokenBucketScript, "Run", redis.NewCmdResult(int64(0), errors.New("mock")))
		defer patches.Reset()

		rLimit := NewShardedRedisTokenBucket(nil, "test", 0, 0, 1, time.Minute)
		ok, err := rLimit.Allow(context.Background(), "key")

		assert.Equal(t, err, errors.New("mock"))
		assert.False(t, ok)
	})

}

func TestShardedRedisTokenBucket_Wait(t *testing.T) {
	t.Run("wait ok", func(t *testing.T) {
		rLimit := NewShardedRedisTokenBucket(nil, "test", 0, 0, 1, time.Minute)

		patches := gomonkey.ApplyMethodReturn(rLimit, "Allow", true, nil)
		defer patches.Reset()

		err := rLimit.Wait(context.Background(), "key", time.Millisecond)

		assert.Nil(t, err)
	})

	t.Run("wait err", func(t *testing.T) {
		rLimit := NewShardedRedisTokenBucket(nil, "test", 0, 0, 1, time.Minute)

		patches := gomonkey.ApplyMethodReturn(rLimit, "Allow", false, nil)
		defer patches.Reset()

		err := rLimit.Wait(context.Background(), "key", time.Millisecond)

		assert.Equal(t, err, ErrTimeout)
	})
}
