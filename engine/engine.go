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

func (e *Engine) Evaluate(ctx context.Context, req domain.FraudCheckRequest) domain.FraudDecision {
	if e.blacklist.IsBlacklisted(ctx, req) {
		return domain.FraudDecision{
			Decision:       "BLOCK",
			RiskScore:      100,
			Reason:         "blacklisted_entity",
			TriggeredRules: []string{"blacklist"},
		}
	}

	score := 0
	triggered := []string{}

	if s, rule := e.velocity.Score(ctx, req); s > 0 {
		score += s
		triggered = append(triggered, rule)
	}

	if s, rule := e.device.Score(ctx, req); s > 0 {
		score += s
		triggered = append(triggered, rule)
	}

	if s, rule := e.amount.Score(req); s > 0 {
		score += s
		triggered = append(triggered, rule)
	}

	return e.scorer.Decide(score, triggered)
}
