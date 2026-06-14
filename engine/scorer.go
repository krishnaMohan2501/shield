package engine

import "github.com/upi/shield/domain"

type Scorer struct {
	blockThreshold  int
	reviewThreshold int
}

func NewScorer(blockThreshold, reviewThreshold int) *Scorer {
	return &Scorer{
		blockThreshold:  blockThreshold,
		reviewThreshold: reviewThreshold,
	}
}

func (s *Scorer) Decide(score int, triggeredRules []string) domain.FraudDecision {
	if triggeredRules == nil {
		triggeredRules = []string{}
	}
	switch {
	case score >= s.blockThreshold:
		return domain.FraudDecision{
			Decision:       "BLOCK",
			RiskScore:      score,
			Reason:         "risk score exceeded block threshold",
			TriggeredRules: triggeredRules,
		}
	case score >= s.reviewThreshold:
		return domain.FraudDecision{
			Decision:       "REVIEW",
			RiskScore:      score,
			Reason:         "risk score exceeded review threshold",
			TriggeredRules: triggeredRules,
		}
	default:
		return domain.FraudDecision{
			Decision:       "ALLOW",
			RiskScore:      score,
			TriggeredRules: triggeredRules,
		}
	}
}
