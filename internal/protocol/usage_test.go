package protocol

import "testing"

func TestUsageEventContract_shouldDefinePrivacyAndUnknownBoundaries(t *testing.T) {
	c := UsageEventContract()
	if c.Version != 1 || len(c.Fields) != 9 || len(c.Outcomes) != 3 || c.UnknownPolicy == "" || c.Privacy == "" {
		t.Fatalf("invalid usage contract: %+v", c)
	}
}
