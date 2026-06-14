package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/upi/shield/api"
	"github.com/upi/shield/config"
	"github.com/upi/shield/engine"
	"github.com/upi/shield/engine/rules"
	"github.com/upi/shield/store"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()

	db  := store.NewPostgres(cfg)
	rdb := store.NewRedis(cfg)
	defer db.Close()  // note: unreachable if log.Fatal(ListenAndServe) calls os.Exit
	defer rdb.Close() // note: unreachable if log.Fatal(ListenAndServe) calls os.Exit

	blacklistRule := rules.NewBlacklistRule(db)
	velocityRule  := rules.NewVelocityRule(rdb, cfg.VelocityMaxTxnPerMinute)
	deviceRule    := rules.NewDeviceRule(db)
	amountRule    := rules.NewAmountRule(cfg.AmountHighValueThreshold)
	scorer        := engine.NewScorer(cfg.RiskBlockThreshold, cfg.RiskReviewThreshold)

	fraudEngine := engine.NewEngine(blacklistRule, velocityRule, deviceRule, amountRule, scorer)
	handler     := api.NewHandler(fraudEngine, db)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /check",  handler.Check)
	mux.HandleFunc("GET /health",  handler.Health)

	addr := ":" + cfg.ServerPort
	log.Printf("[SHIELD] Starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
