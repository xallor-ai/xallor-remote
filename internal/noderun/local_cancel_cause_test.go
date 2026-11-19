package noderun

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Only the authenticated Relay's explicit revocation reason maps to loss of
// authority. Ordinary cancel remains user cancellation; unknown reasons do not
// cancel an invocation or affect another local execution.
func TestLocalCancel_shouldPreserveTrustedCause_andIgnoreUnknownReason(t *testing.T) {
	for _, reason := range []string{"", protocol.Unauthorized, "unrecognized"} {
		t.Run("reason_"+reason, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			other, cancelOther := context.WithCancelCause(context.Background())
			defer cancelOther(nil)
			d := &Daemon{approvals: &approvalHub{}, local: map[string]context.CancelCauseFunc{"owned": cancel, "other": cancelOther}}
			d.onDeviceMsg(nil, protocol.Message{Type: protocol.TypeInvoke, Op: protocol.OpCancel, ExecID: "owned", Code: reason})
			switch reason {
			case protocol.Unauthorized:
				if !errors.Is(context.Cause(ctx), delegation.ErrDenied) {
					t.Fatal("authorization revocation became ordinary cancellation")
				}
			case "":
				if !errors.Is(context.Cause(ctx), context.Canceled) {
					t.Fatal("ordinary cancellation changed")
				}
			default:
				if ctx.Err() != nil {
					t.Fatal("unknown reason cancelled execution")
				}
			}
			if other.Err() != nil {
				t.Fatal("unrelated execution cancelled")
			}
		})
	}
}
