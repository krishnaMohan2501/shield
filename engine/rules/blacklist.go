package rules

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/upi/shield/domain"
)

// BlacklistRule keeps an in-memory copy of all active blacklist entries and
// refreshes it from Postgres every 30 seconds. Hot-path checks are pure map
// lookups under a read lock — no DB round-trip per request.
type BlacklistRule struct {
	db  *sql.DB
	mu  sync.RWMutex
	set map[string]struct{} // "TYPE:value" entries
}

func NewBlacklistRule(db *sql.DB) *BlacklistRule {
	r := &BlacklistRule{db: db, set: make(map[string]struct{})}
	r.load() // synchronous initial load so cache is warm before first request
	go r.refreshLoop()
	return r
}

func (r *BlacklistRule) load() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rows, err := r.db.QueryContext(ctx,
		`SELECT type, value FROM blacklist WHERE expires_at IS NULL OR expires_at > NOW()`)
	if err != nil {
		// Fail-closed: keep the existing (possibly stale) cache intact.
		log.Printf("[SHIELD] blacklist cache refresh error: %v", err)
		return
	}
	defer rows.Close()

	newSet := make(map[string]struct{})
	for rows.Next() {
		var typ, val string
		if rows.Scan(&typ, &val) == nil {
			newSet[typ+":"+val] = struct{}{}
		}
	}

	r.mu.Lock()
	r.set = newSet
	r.mu.Unlock()
	log.Printf("[SHIELD] blacklist cache refreshed: %d entries", len(newSet))
}

func (r *BlacklistRule) refreshLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		r.load()
	}
}

func (r *BlacklistRule) IsBlacklisted(_ context.Context, req domain.FraudCheckRequest) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, vpa := r.set["VPA:"+req.ReceiverVPA]
	_, ip := r.set["IP:"+req.IPAddress]
	_, dev := r.set["DEVICE:"+req.DeviceID]
	return vpa || ip || dev
}
