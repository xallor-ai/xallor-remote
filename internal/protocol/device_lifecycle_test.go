package protocol

import "testing"

// Lifecycle steps cover enrollment and every destructive/ownership transition;
// each step has explicit unknown-outcome handling and scope effects.
func TestDeviceLifecycle_should_defineStableFailClosedTransitions(t *testing.T) {
	c := DeviceLifecycle()
	if c.Version != 1 || len(c.Steps) != 6 {
		t.Fatalf("unexpected lifecycle contract: %+v", c)
	}
	for _, step := range c.Steps {
		if step.Name == "" || step.Actor == "" || step.Precondition == "" || step.Effect == "" || step.Scope == "" || step.OnUnknown == "" {
			t.Fatalf("incomplete lifecycle step: %+v", step)
		}
	}
}
