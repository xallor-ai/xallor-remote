package protocol

import "testing"

// Commercial features remain a separate contract from transport and local job safety limits.
func TestEntitlementContract_shouldSeparateCommercialAndSafetyBudgets(t *testing.T) {
	c := EntitlementContract()
	if c.Version != 1 || c.LocalJobLedger != "safety_capacity_only" || c.TransportLimits == "" || len(c.Features) == 0 {
		t.Fatalf("invalid entitlement contract: %+v", c)
	}
}
