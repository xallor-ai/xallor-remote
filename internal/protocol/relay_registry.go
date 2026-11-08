package protocol

type RelayRegistryContract struct {
	Version          int    `json:"version"`
	Authority        string `json:"authority"`
	DeviceKey        string `json:"device_key"`
	StickyRoute      string `json:"sticky_route"`
	Reconnect        string `json:"reconnect"`
	Disconnect       string `json:"disconnect"`
	StaleEvent       string `json:"stale_event"`
	Health           string `json:"health"`
	HighAvailability string `json:"high_availability"`
}

// RelayRegistry describes the single-Relay routing boundary. It is not a
// topology or failover promise; multi-region ownership belongs to #70.
func RelayRegistry() RelayRegistryContract {
	return RelayRegistryContract{
		Version:          1,
		Authority:        "persistent_relay_authority",
		DeviceKey:        "display_device_id plus authenticated secret, mapped to non-reusable device_uid",
		StickyRoute:      "one active device connection per device_uid; clients bind to target and access incarnation",
		Reconnect:        "new authenticated connection queues hello before replacing old route and receives current settings",
		Disconnect:       "fail in-flight routes, remove online mapping, release transport leases, retain durable identity",
		StaleEvent:       "old connection and old route must not deliver to a replacement binding",
		Health:           "process/build liveness only; does not claim device or execution readiness",
		HighAvailability: "not implemented; one Relay authority and in-memory online registry",
	}
}
