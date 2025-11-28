package limiter

import "context"

// Limiter is a simple interface for limiting concurrent access to a resource.
type Limiter interface {
	Wait(ctx context.Context) (err error)
	Done(ctx context.Context)
}

type nopLimiter struct{}

// NewNopLimiter returns a no-op limiter.
func NewNopLimiter() Limiter { return &nopLimiter{} }

// Wait 方法在nopLimiter中实现，它接收一个context.Context类型的参数ctx，返回一个error类型的值
func (l *nopLimiter) Wait(_ context.Context) (err error) {
	return nil
}

// Done 方法在nopLimiter中实现，它接收一个context.Context类型的参数ctx
func (l *nopLimiter) Done(_ context.Context) {}
