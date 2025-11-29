package limiter

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"
)

func TestSingleSlidingWindowLimiter_AllowN(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	ctx := context.Background()

	t.Run("SingleSlidingWindowLimiter_AllowN_ok", func(t *testing.T) {
		sha := slidingWindowScript.Hash() // 你也需要暴露 hash
		nowMs := float64(time.Now().UnixNano() / 1e6)

		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetVal(int64(1))

		sw := NewSlidingWindowLimiter(
			db,
			"login",
			WithSlidingWindowWindow(time.Minute),
			WithSlidingWindowLimit(60),
			WithSlidingWindowTTL(2*time.Minute),
		)

		ok, err := sw.Allow(ctx)
		assert.Nil(t, err)
		assert.True(t, ok)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SingleSlidingWindowLimiter_AllowN_err", func(t *testing.T) {
		sha := slidingWindowScript.Hash() // 你也需要暴露 hash
		nowMs := float64(time.Now().UnixNano() / 1e6)

		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetErr(redis.ErrClosed)

		sw := NewSlidingWindowLimiter(
			db,
			"login",
			WithSlidingWindowWindow(time.Minute),
			WithSlidingWindowLimit(60),
			WithSlidingWindowTTL(2*time.Minute),
		)

		ok, err := sw.Allow(ctx)
		assert.ErrorIs(t, err, redis.ErrClosed)
		assert.False(t, ok)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SingleSlidingWindowLimiter_AllowN_Result_err", func(t *testing.T) {
		sha := slidingWindowScript.Hash() // 你也需要暴露 hash
		nowMs := float64(time.Now().UnixNano() / 1e6)

		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetVal("0")

		sw := NewSlidingWindowLimiter(
			db,
			"login",
			WithSlidingWindowWindow(time.Minute),
			WithSlidingWindowLimit(60),
			WithSlidingWindowTTL(2*time.Minute),
		)

		ok, err := sw.Allow(ctx)
		assert.ErrorContains(t, err, "sliding window: unexpected script result")
		assert.False(t, ok)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSingleSlidingWindowLimiter_Wait(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()
	ctx := context.Background()

	sw := NewSlidingWindowLimiter(
		db,
		"login",
		WithSlidingWindowWindow(time.Minute),
		WithSlidingWindowLimit(60),
		WithSlidingWindowTTL(2*time.Minute),
	)

	t.Run("SingleSlidingWindowLimiter_Wait_ok", func(t *testing.T) {
		sha := slidingWindowScript.Hash() // 你也需要暴露 hash
		nowMs := float64(time.Now().UnixNano() / 1e6)

		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetVal(0)
		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetVal(1)

		err := sw.Wait(ctx, time.Second)
		assert.Nil(t, err)
	})

	t.Run("SingleSlidingWindowLimiter_Wait_Err", func(t *testing.T) {
		sha := slidingWindowScript.Hash() // 你也需要暴露 hash
		nowMs := float64(time.Now().UnixNano() / 1e6)

		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[5] = nowMs
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectEvalSha(
			sha,
			[]string{
				"sw:{login}:log",
				"sw:{login}:seq",
			},
			nowMs,
			int64(60_000), // windowMs
			int64(60),     // limit
			int64(120_000),
		).SetVal(0)

		err := sw.Wait(ctx, 0)
		assert.ErrorIs(t, err, ErrLimiter)
	})
}

func TestSingleSlidingWindowLimiter_State(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()
	ctx := context.Background()

	sw := NewSlidingWindowLimiter(
		db,
		"login",
		WithSlidingWindowWindow(time.Minute),
		WithSlidingWindowLimit(60),
		WithSlidingWindowTTL(2*time.Minute),
	)

	t.Run("SingleSlidingWindowLimiter_State_OK", func(t *testing.T) {
		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[2] = expected[2]
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectZCount("sw:{login}:log", "", "+inf").SetVal(10)

		state, err := sw.State(ctx)
		assert.Nil(t, err)
		assert.Equal(t, float64(60), state.Capacity)
	})
	t.Run("SingleSlidingWindowLimiter_State_Err", func(t *testing.T) {
		mock.CustomMatch(func(expected, actual []interface{}) error {
			actual[2] = expected[2]
			if !reflect.DeepEqual(expected, actual) {
				return fmt.Errorf("expected %v, got %v", expected, actual)
			}
			return nil
		}).ExpectZCount("sw:{login}:log", "", "+inf").SetErr(redis.ErrClosed)

		state, err := sw.State(ctx)
		assert.ErrorIs(t, err, redis.ErrClosed)
		assert.Equal(t, float64(0), state.Capacity)

	})
}
