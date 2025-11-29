package limiter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"
)

func TestTokenBucket_Allow(t *testing.T) {
	db, mock := redismock.NewClientMock()
	ctx := context.Background()

	t.Run("TokenBucket_Allow_ok", func(t *testing.T) {
		// 使用你的 tokenBucketScript.Hash() 计算脚本 SHA
		sha := tokenBucketScript.Hash() // 你需要暴露该方法，见下方说明

		nowMs := float64(time.Now().UnixNano() / 1e6)

		// 匹配脚本运行参数
		mock.ExpectEvalSha(
			sha,
			[]string{
				"tbucket:{test}:tokens",
				"tbucket:{test}:ts",
			},
			nowMs,
			100.0, // Rate
			100.0, // Capacity
			1.0,   // Request tokens
			int64(2000),
		).SetVal(int64(1))

		tb := NewTokenBucketLimiter(
			db,
			"test",
			WithTokenBucketRate(100),
			WithTokenBucketCapacity(100),
			WithTokenBucketTTL(2*time.Second),
		)

		ok, err := tb.Allow(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("should allow")
		}

		// 验证 mock 是否被调度完
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}

func TestTokenBucket_State(t *testing.T) {
	db, mock := redismock.NewClientMock()
	ctx := context.Background()

	t.Run("TokenBucket_State_ok", func(t *testing.T) {
		now := time.Now().UnixMilli()

		// 模拟 tokensKey = "50"
		mock.ExpectGet("tbucket:{state}:tokens").SetVal("50")
		// 上次更新时间 tsKey = now
		mock.ExpectGet("tbucket:{state}:ts").SetVal(
			fmt.Sprintf("%d", now),
		)

		tb := NewTokenBucketLimiter(
			db,
			"state",
			WithTokenBucketRate(100),
			WithTokenBucketCapacity(100),
		)

		s, err := tb.State(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if s.Level < 50 {
			t.Fatalf("expected token >= 50, got %.2f", s.Level)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
	t.Run("TokenBucket_State_fail", func(t *testing.T) {
		// 模拟 tokensKey = "50"
		mock.ExpectGet("tbucket:{state}:tokens").SetErr(redis.Nil)
		tb := NewTokenBucketLimiter(
			db,
			"state",
			WithTokenBucketRate(100),
			WithTokenBucketCapacity(100),
		)

		s, err := tb.State(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assert.Equal(t, s.Level, float64(100))
	})

	t.Run("TokenBucket_State_tokens_fail", func(t *testing.T) {
		// 模拟 tokensKey = "50"
		mock.ExpectGet("tbucket:{state}:tokens").SetVal("50")
		// 上次更新时间 tsKey = now
		mock.ExpectGet("tbucket:{state}:ts").SetErr(redis.Nil)

		tb := NewTokenBucketLimiter(
			db,
			"state",
			WithTokenBucketRate(100),
			WithTokenBucketCapacity(100),
		)

		s, err := tb.State(ctx)
		assert.ErrorIs(t, err, redis.Nil)
		assert.Equal(t, s.Level, float64(0))
	})
}

func TestTokenBucketLimiter_Wait(t *testing.T) {
	db, _ := redismock.NewClientMock()
	ctx := context.Background()

	tb := NewTokenBucketLimiter(
		db,
		"test",
		WithTokenBucketRate(100),
		WithTokenBucketCapacity(100),
		WithTokenBucketTTL(2*time.Second),
	)

	t.Run("TokenBucket_Wait_ok", func(t *testing.T) {
		patches := gomonkey.ApplyMethodSeq(tb, "AllowN", []gomonkey.OutputCell{
			{Values: gomonkey.Params{false, nil}},
			{Values: gomonkey.Params{true, nil}},
		})
		defer patches.Reset()

		err := tb.Wait(ctx, time.Second)

		assert.NoError(t, err)
	})

	t.Run("TokenBucket_Wait_fail", func(t *testing.T) {
		patches := gomonkey.ApplyMethodSeq(tb, "AllowN", []gomonkey.OutputCell{
			{Values: gomonkey.Params{false, nil}},
		})
		defer patches.Reset()

		err := tb.Wait(ctx, 0)

		assert.Error(t, err, ErrTimeout)
	})
}
