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
