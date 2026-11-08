package protocol

import "testing"

func TestRelayFailover_shouldSeparateHealthOwnershipAndUnknownExecution(t *testing.T) {
	c := RelayFailover()
	if c.Version != 1 || len(c.HealthStates) != 4 || c.DeviceOwnership == "" || c.RouteHealth == "" || c.UnknownExecution == "" || c.Replay == "" {
		t.Fatalf("invalid failover contract: %+v", c)
	}
}
