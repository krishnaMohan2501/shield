package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/lib/pq"
	"github.com/upi/shield/domain"
	"github.com/upi/shield/engine"
)

type Handler struct {
	engine *engine.Engine
	db     *sql.DB
}

func NewHandler(e *engine.Engine, db *sql.DB) *Handler {
	return &Handler{engine: e, db: db}
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req domain.FraudCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Millisecond)
	defer cancel()

	decision := h.engine.Evaluate(ctx, req)

	log.Printf("[SHIELD] %s | req=%s | user=%s | score=%d | rules=%v | took=%s",
		decision.Decision, req.RequestID, req.UserID,
		decision.RiskScore, decision.TriggeredRules, time.Since(start))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(decision); err != nil {
		log.Printf("[SHIELD] encode error: %v", err)
	}

	go h.saveAuditLog(req, decision)
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) saveAuditLog(req domain.FraudCheckRequest, decision domain.FraudDecision) {
	_, err := h.db.Exec(`
		INSERT INTO fraud_audit_log (request_id, user_id, decision, risk_score, triggered_rules)
		VALUES ($1, $2, $3, $4, $5)
	`, req.RequestID, req.UserID, decision.Decision, decision.RiskScore,
		pq.Array(decision.TriggeredRules))

	if err != nil {
		log.Printf("[SHIELD] audit log error: %v", err)
	}
}
