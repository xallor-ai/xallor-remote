package protocol

type RelayFailoverContract struct {
	Version          int      `json:"version"`
	HealthStates     []string `json:"health_states"`
	DeviceOwnership  string   `json:"device_ownership"`
	StickyPolicy     string   `json:"sticky_policy"`
	RouteHealth      string   `json:"route_health"`
	UnknownExecution string   `json:"unknown_execution"`
	Replay           string   `json:"replay"`
	Implementation   string   `json:"implementation"`
}

// RelayFailover describes the future multi-region boundary without pretending
// that a local health probe or a disconnected route proves execution safety.
func RelayFailover() RelayFailoverContract {
	return RelayFailoverContract{
		Version:          1,
		HealthStates:     []string{"healthy", "degraded", "unavailable", "unknown"},
		DeviceOwnership:  "one authority owns a device_uid at a time; transfer requires durable registry evidence",
		StickyPolicy:     "keep a device on its authority/region until an explicit ownership decision; do not route by display ID alone",
		RouteHealth:      "process liveness, registry liveness, device readiness, and execution route health are separate",
		UnknownExecution: "preserve unknown after disconnect or missing terminal; never infer stopped or completed",
		Replay:           "never automatically replay an in-flight command or handoff with side effects",
		Implementation:   "provider-neutral contract only; multi-region routing, replication and failover are not implemented",
	}
}
