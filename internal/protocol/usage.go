package protocol

type UsageContract struct {
	Version       int      `json:"version"`
	Fields        []string `json:"fields"`
	Outcomes      []string `json:"outcomes"`
	Idempotency   string   `json:"idempotency"`
	UnknownPolicy string   `json:"unknown_policy"`
	Privacy       string   `json:"privacy"`
}

func UsageEventContract() UsageContract {
	return UsageContract{
		Version:       1,
		Fields:        []string{"id", "at", "idempotency_key", "tenant_id", "membership_id", "device_uid", "operation", "outcome", "units"},
		Outcomes:      []string{"completed", "failed", "unknown"},
		Idempotency:   "same key and same event is a no-op; conflicting reuse is rejected",
		UnknownPolicy: "retain for reconciliation; never count as completed or billable",
		Privacy:       "metadata only; no command, path, prompt, output, token, IP, or payment data",
	}
}
