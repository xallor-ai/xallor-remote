package protocol

type EntitlementFeature struct {
	Name  string `json:"name"`
	Unit  string `json:"unit"`
	Scope string `json:"scope"`
}

type EntitlementContractDefinition struct {
	Version         int                  `json:"version"`
	Features        []EntitlementFeature `json:"features"`
	LocalJobLedger  string               `json:"local_job_ledger"`
	TransportLimits string               `json:"transport_limits"`
}

// EntitlementContract describes commercial values without claiming that this
// Runtime has a billing provider or a current tenant snapshot.
func EntitlementContract() EntitlementContractDefinition {
	return EntitlementContractDefinition{
		Version: 1,
		Features: []EntitlementFeature{
			{Name: "exec.concurrent", Unit: "jobs", Scope: "tenant/membership"},
			{Name: "terminal.concurrent", Unit: "sessions", Scope: "tenant/membership"},
			{Name: "relay.bytes_per_second", Unit: "bytes_per_second", Scope: "tenant"},
			{Name: "relay.monthly_egress_bytes", Unit: "bytes", Scope: "tenant"},
			{Name: "file.max_transfer_bytes", Unit: "bytes", Scope: "tenant/membership"},
		},
		LocalJobLedger:  "safety_capacity_only",
		TransportLimits: "inflight_ip_and_frame_hard_caps",
	}
}
