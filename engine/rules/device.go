package rules

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/upi/shield/domain"
)

// DeviceRule caches known user+device pairs in memory. After the first
// successful registration, subsequent requests for the same pair skip the
// Postgres INSERT entirely — eliminating the DB round-trip on the hot path.
// The cache is intentionally not persisted: on restart, the first request
// per device re-runs the INSERT (which no-ops via ON CONFLICT), then caches.
type DeviceRule struct {
	db    *sql.DB
	cache sync.Map // key: "userID:deviceID"
}

func NewDeviceRule(db *sql.DB) *DeviceRule {
	return &DeviceRule{db: db}
}

func (r *DeviceRule) Score(_ context.Context, req domain.FraudCheckRequest) (int, string) {
	key := req.UserID + ":" + req.DeviceID

	// LoadOrStore atomically: if key was already present, device is known → no score.
	// If we just stored it, this goroutine "won" the first-seen race → score +30.
	if _, loaded := r.cache.LoadOrStore(key, struct{}{}); loaded {
		return 0, ""
	}

	// Device is genuinely new: return the score immediately and persist async.
	// Using Background context so the INSERT outlives the request context.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO device_registry (device_id, user_id)
			VALUES ($1, $2)
			ON CONFLICT (device_id, user_id) DO NOTHING
		`, req.DeviceID, req.UserID)
		if err != nil {
			log.Printf("[SHIELD] device registration error: %v", err)
			r.cache.Delete(key) // allow retry on next request
		}
	}()

	return 30, "unknown_device"
}
