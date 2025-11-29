package limiter

type ShardedTokenBucketOption func(*ShardedTokenBucketLimiter)

func WithShardCount(count int) ShardedTokenBucketOption {
	return func(s *ShardedTokenBucketLimiter) {
		if count > 0 {
			s.count = count
		}
	}
}
