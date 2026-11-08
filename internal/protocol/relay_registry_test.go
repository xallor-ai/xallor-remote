package protocol

import "testing"

func TestRelayRegistry_shouldMakeStickyAndStaleRouteBoundariesExplicit(t *testing.T) {
	c := RelayRegistry()
	if c.Version != 1 || c.DeviceKey == "" || c.StickyRoute == "" || c.StaleEvent == "" || c.Health == "" || c.HighAvailability == "" {
		t.Fatalf("invalid registry contract: %+v", c)
	}
}
