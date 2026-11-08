package protocol

const (
	MaxUsageEvents      = 5000
	MaxUsageRetiredKeys = 5000
	MaxUsageBytes       = 4 * 1024 * 1024
	UsageMinTime        = "1970-01-01T00:00:00Z"
	UsageMaxTime        = "9999-12-31T23:59:59.999999999Z"
	MaxUsageTimeBytes   = 30
)

type UsageTimeContract struct {
	Format    string `json:"format"`
	Zone      string `json:"zone"`
	Min       string `json:"min"`
	Max       string `json:"max"`
	MaxBytes  int    `json:"max_bytes"`
	Canonical bool   `json:"canonical"`
}

type UsageRetentionContract struct {
	MaxEvents      int    `json:"max_events"`
	MaxRetiredKeys int    `json:"max_retired_keys"`
	MaxBytes       int    `json:"max_bytes"`
	RetiredKey     string `json:"retired_key"`
	WhenFull       string `json:"when_full"`
}

type UsageContract struct {
	Version       int                    `json:"version"`
	Fields        []string               `json:"fields"`
	Outcomes      []string               `json:"outcomes"`
	Idempotency   string                 `json:"idempotency"`
	UnknownPolicy string                 `json:"unknown_policy"`
	Privacy       string                 `json:"privacy"`
	Time          UsageTimeContract      `json:"time"`
	Retention     UsageRetentionContract `json:"retention"`
	Aggregation   string                 `json:"aggregation"`
}

func UsageEventContract() UsageContract {
	return UsageContract{
		Version:       2,
		Fields:        []string{"id", "at", "idempotency_key", "tenant_id", "membership_id", "device_uid", "operation", "outcome", "units"},
		Outcomes:      []string{"completed", "failed", "unknown"},
		Idempotency:   "retained same key and same event is a no-op; conflicting or retired key reuse is rejected",
		UnknownPolicy: "retain for reconciliation; never count as completed or billable",
		Privacy:       "metadata only; no command, path, prompt, output, token, IP, or payment data",
		Time:          UsageTimeContract{Format: "RFC3339Nano", Zone: "UTC Z", Min: UsageMinTime, Max: UsageMaxTime, MaxBytes: MaxUsageTimeBytes, Canonical: true},
		Retention: UsageRetentionContract{
			MaxEvents: MaxUsageEvents, MaxRetiredKeys: MaxUsageRetiredKeys, MaxBytes: MaxUsageBytes,
			RetiredKey: "durable SHA-256 tombstone; all reuse conflicts, including identical event",
			WhenFull:   "reject append requiring another retirement; never discard tombstones or reset automatically",
		},
		Aggregation: "retained completed observations only, exact tenant/membership scope; int64 overflow returns error and zero, never a partial total",
	}
}
