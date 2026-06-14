package rules

import "github.com/upi/shield/domain"

type AmountRule struct {
	threshold float64
}

func NewAmountRule(threshold float64) *AmountRule {
	return &AmountRule{threshold: threshold}
}

func (r *AmountRule) Score(req domain.FraudCheckRequest) (int, string) {
	if req.Amount > r.threshold {
		return 20, "high_amount"
	}
	return 0, ""
}
