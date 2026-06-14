package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/upi/shield/config"
)

func NewPostgres(cfg config.Config) *sql.DB {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("[SHIELD] postgres open error: %v", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("[SHIELD] postgres ping error: %v", err)
	}

	initSchema(db)
	return db
}

func initSchema(db *sql.DB) {
	_, err := db.Exec(`
		CREATE EXTENSION IF NOT EXISTS "pgcrypto";

		CREATE TABLE IF NOT EXISTS blacklist (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			type       VARCHAR(10)  NOT NULL,
			value      VARCHAR(255) NOT NULL,
			reason     TEXT,
			added_at   TIMESTAMPTZ  DEFAULT NOW(),
			expires_at TIMESTAMPTZ
		);

		CREATE TABLE IF NOT EXISTS device_registry (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			device_id     VARCHAR(255) NOT NULL,
			user_id       VARCHAR(255) NOT NULL,
			first_seen_at TIMESTAMPTZ  DEFAULT NOW(),
			is_trusted    BOOLEAN      DEFAULT FALSE,
			UNIQUE(device_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS fraud_audit_log (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			request_id      VARCHAR(255) NOT NULL,
			user_id         VARCHAR(255) NOT NULL,
			decision        VARCHAR(10)  NOT NULL,
			risk_score      INT          NOT NULL,
			triggered_rules TEXT[],
			created_at      TIMESTAMPTZ  DEFAULT NOW()
		);
	`)
	if err != nil {
		log.Fatalf("[SHIELD] schema init error: %v", err)
	}
	log.Println("[SHIELD] Postgres schema ready")
}
