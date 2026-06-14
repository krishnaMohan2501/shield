package engine

import (
	"context"

	"github.com/upi/shield/domain"
	"github.com/upi/shield/engine/rules"
)

type Engine struct {
	blacklist *rules.BlacklistRule
	velocity  *rules.VelocityRule
	device    *rules.DeviceRule
	amount    *rules.AmountRule
	scorer    *Scorer
}

func NewEngine(
	blacklist *rules.BlacklistRule,
	velocity  *rules.VelocityRule,
	device    *rules.DeviceRule,
	amount    *rules.AmountRule,
	scorer    *Scorer,
) *Engine {
	return &Engine{
		blacklist: blacklist,
		velocity:  velocity,
		device:    device,
		amount:    amount,
		scorer:    scorer,
	}
}

type ruleResult struct {
	score int
	rule  string
}

func (e *Engine) Evaluate(ctx context.Context, req domain.FraudCheckRequest) domain.FraudDecision {
	if e.blacklist.IsBlacklisted(ctx, req) {
		return domain.FraudDecision{
			Decision:       "BLOCK",
			RiskScore:      100,
			Reason:         "blacklisted_entity",
			TriggeredRules: []string{"blacklist"},
		}
	}

	// Velocity (Redis) and device (Postgres or memory cache) run concurrently.
	velCh := make(chan ruleResult, 1)
	devCh := make(chan ruleResult, 1)

	go func() {
		s, r := e.velocity.Score(ctx, req)
		velCh <- ruleResult{s, r}
	}()
	go func() {
		s, r := e.device.Score(ctx, req)
		devCh <- ruleResult{s, r}
	}()

	vel := <-velCh
	dev := <-devCh

	score := vel.score + dev.score
	triggered := []string{}
	if vel.rule != "" {
		triggered = append(triggered, vel.rule)
	}
	if dev.rule != "" {
		triggered = append(triggered, dev.rule)
	}

	if s, rule := e.amount.Score(req); s > 0 {
		score += s
		triggered = append(triggered, rule)
	}

	return e.scorer.Decide(score, triggered)
}
