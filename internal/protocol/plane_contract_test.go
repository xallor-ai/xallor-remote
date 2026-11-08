package protocol

import "testing"

func TestControlDataPlane_shouldKeepManagementAndRealtimeResponsibilitiesSeparate(t *testing.T) {
	c := ControlDataPlane()
	if c.Version != 1 || c.ControlEntry == "" || c.DataTransport == "" || c.ManagementOperations == "" || c.DataOperations == "" || c.CredentialBoundary == "" || c.UnknownPolicy == "" {
		t.Fatalf("invalid plane contract: %+v", c)
	}
}
