package rules

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/upi/shield/domain"
)

type DeviceRule struct {
	db *sql.DB
}

func NewDeviceRule(db *sql.DB) *DeviceRule {
	return &DeviceRule{db: db}
}

func (r *DeviceRule) Score(ctx context.Context, req domain.FraudCheckRequest) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()

	result, err := r.db.ExecContext(ctx, `
		INSERT INTO device_registry (device_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (device_id, user_id) DO NOTHING
	`, req.DeviceID, req.UserID)

	if err != nil {
		log.Printf("[SHIELD] device rule error: %v", err)
		return 0, ""
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		return 30, "unknown_device"
	}
	return 0, ""
}
