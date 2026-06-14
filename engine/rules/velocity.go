package rules

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/upi/shield/domain"
)

type VelocityRule struct {
	redis   *redis.Client
	maxTxns int
}

func NewVelocityRule(redis *redis.Client, maxTxns int) *VelocityRule {
	return &VelocityRule{redis: redis, maxTxns: maxTxns}
}

func (r *VelocityRule) Score(ctx context.Context, req domain.FraudCheckRequest) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	key := "fraud:velocity:" + req.UserID

	count, err := r.redis.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("[SHIELD] velocity incr error: %v", err)
		return 0, ""
	}

	if count == 1 {
		// Use a fresh context: the parent 10ms may be nearly exhausted after INCR.
		// If EXPIRE fails, delete the key to prevent a permanent never-expiring counter.
		expCtx, expCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer expCancel()
		if err := r.redis.Expire(expCtx, key, 60*time.Second).Err(); err != nil {
			log.Printf("[SHIELD] velocity expire failed for %s: %v — deleting key", key, err)
			r.redis.Del(context.Background(), key)
		}
	}

	if count > int64(r.maxTxns) {
		return 40, "velocity_exceeded"
	}
	return 0, ""
}
