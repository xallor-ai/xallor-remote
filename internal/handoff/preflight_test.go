package handoff

import (
	"context"
	"testing"
	"time"
)

// Cancellation/deadline are operational uncertainty, not proof of an incompatible CLI/repository.
func TestPreflight_should_preserveInterruptionCause(t *testing.T) {
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		code := "preflight_interrupted"
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			code = "preflight_timeout"
		}
		cancel()
		m := New(context.Background(), t.TempDir(), Hooks{})
		workspace := t.TempDir()
		_, _, _, err := m.preflight(ctx, testRequest(workspace), workspace)
		m.Close()
		if err == nil || err.Error() != code {
			t.Fatalf("expected %s, got %v", code, err)
		}
	}
}
