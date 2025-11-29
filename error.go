package limiter

import "fmt"

var (
	ErrLimiter = fmt.Errorf("rate limit exceeded")
	ErrTimeout = fmt.Errorf("rate limited (timeout)")
)
