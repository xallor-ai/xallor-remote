package release

import (
	"strings"
	"testing"
)

// A complete ready wire identity includes the launch instance, not just the
// artifact/activation. Go heads reject a missing or malformed launch ID.
func TestReadyObservation_should_requireLaunchIdentity(t *testing.T) {
	v := RuntimeObservation{State: "ready", Version: "0.2.6", Revision: strings.Repeat("a", 40),
		Protocol: 1, ProfileFormat: 1, SHA256: strings.Repeat("b", 64), PID: 1,
		ActivationID: strings.Repeat("c", 32), LaunchID: strings.Repeat("d", 32)}
	if err := v.ValidateReady(1, 1); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "short", strings.Repeat("D", 32)} {
		v.LaunchID = invalid
		if err := v.ValidateReady(1, 1); err == nil {
			t.Fatal("incomplete launch accepted", invalid)
		}
	}
}
