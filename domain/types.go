package domain

type FraudCheckRequest struct {
	RequestID   string  `json:"requestId"`
	UserID      string  `json:"userId"`
	DeviceID    string  `json:"deviceId"`
	IPAddress   string  `json:"ipAddress"`
	Action      string  `json:"action"`
	Amount      float64 `json:"amount"`
	ReceiverVPA string  `json:"receiverVpa"`
}

type FraudDecision struct {
	Decision       string   `json:"decision"`
	RiskScore      int      `json:"riskScore"`
	Reason         string   `json:"reason,omitempty"`
	TriggeredRules []string `json:"triggeredRules"`
}
