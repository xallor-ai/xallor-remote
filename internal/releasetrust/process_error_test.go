package releasetrust

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Synthetic boundary errors test classification and redaction, not actual OS
// cleanup failure. Native descendant settlement is covered separately.
func TestProofProcessError_shouldPreserveUncertainty_withoutLeakingToolDetails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cause     error
		uncertain bool
	}{
		{"cleanup", process.ErrCleanup, true},
		{"wait-delay", exec.ErrWaitDelay, true},
		{"ordinary-refusal", errors.New("exit status 1"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := proofProcessError(errors.Join(tc.cause, errors.New("secret-tool-output")))
			if !errors.Is(err, ErrUnverified) || errors.Is(err, ErrCleanupUncertain) != tc.uncertain {
				t.Fatalf("wrong classification: %v", err)
			}
			if strings.Contains(err.Error(), "secret-tool-output") {
				t.Fatal("tool details leaked")
			}
		})
	}
}
