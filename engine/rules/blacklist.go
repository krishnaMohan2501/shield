package rules

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/upi/shield/domain"
)

type BlacklistRule struct {
	db *sql.DB
}

func NewBlacklistRule(db *sql.DB) *BlacklistRule {
	return &BlacklistRule{db: db}
}

func (r *BlacklistRule) IsBlacklisted(ctx context.Context, req domain.FraudCheckRequest) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()

	var exists int
	err := r.db.QueryRowContext(ctx, `
		SELECT 1 FROM blacklist
		WHERE ((type = 'VPA'    AND value = $1)
		    OR (type = 'IP'     AND value = $2)
		    OR (type = 'DEVICE' AND value = $3))
		   AND (expires_at IS NULL OR expires_at > NOW())
		LIMIT 1
	`, req.ReceiverVPA, req.IPAddress, req.DeviceID).Scan(&exists)

	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		// Fail-closed: if we can't verify the blacklist, block the request.
		// A temporary DB error should never allow a known fraudster through.
		log.Printf("[SHIELD] blacklist query error (blocking): %v", err)
		return true
	}
	return true
}
