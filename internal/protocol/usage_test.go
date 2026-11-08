package protocol

import "testing"

// Static CLI/target/MCP metadata must expose bounded time, conflict retention
// and checked aggregation while preserving privacy and unknown-outcome rules.
func TestUsageEventContract_shouldDefinePrivacyAndUnknownBoundaries(t *testing.T) {
	c := UsageEventContract()
	if c.Version != 2 || len(c.Fields) != 9 || len(c.Outcomes) != 3 || c.UnknownPolicy == "" || c.Privacy == "" {
		t.Fatalf("invalid usage contract: %+v", c)
	}
	if c.Time.Format != "RFC3339Nano" || c.Time.Zone != "UTC Z" || !c.Time.Canonical || c.Time.Min != UsageMinTime || c.Time.Max != UsageMaxTime || c.Time.MaxBytes != MaxUsageTimeBytes {
		t.Fatalf("invalid usage time contract: %+v", c.Time)
	}
	if c.Retention.MaxEvents != MaxUsageEvents || c.Retention.MaxRetiredKeys != MaxUsageRetiredKeys || c.Retention.MaxBytes != MaxUsageBytes || c.Retention.RetiredKey == "" || c.Retention.WhenFull == "" || c.Aggregation == "" {
		t.Fatalf("unbounded usage retention or unchecked aggregation: %+v", c)
	}
}
