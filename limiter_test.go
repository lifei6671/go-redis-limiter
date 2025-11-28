package limiter

import (
	"context"
	"testing"
)

func TestNopLimiter(t *testing.T) {
	l := NewNopLimiter()
	defer l.Done(context.Background())
	if l.Wait(context.Background()) != nil {
		t.Error("NopLimiter should not return an error")
	}
}
