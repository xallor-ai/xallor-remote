//go:build windows

package releasetrust

import (
	"context"
	"testing"
)

// Provenance checks are background operations, not interactive console launches.
func TestProofCommandSuppressesConsole(t *testing.T) {
	cmd := proofCommand(context.Background(), "not-executed.exe", "attestation", "verify")
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("proof verification can create a console")
	}
}
