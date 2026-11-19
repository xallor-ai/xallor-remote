package noderun

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Force background startup polling to be delayed. A ready reply itself must
// latch admission, otherwise a subsequent install's pending kills this owner.
func TestRuntimeOwner_should_latchStartupBeforeReadyReply(t *testing.T) {
	d := New(ownerTestStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer d.Stop()
	selected := true
	checks := 0
	launchID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	d.WithLaunchIdentity(launchID)
	d.WithStartupGate(func() (bool, error) { checks++; return selected, nil })
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	d.startOwnerObservation(ipc.NewConn(server), ipc.Request{ID: "ack", Method: "runtime.ensure"},
		func(context.Context) (release.RuntimeObservation, error) {
			return release.RuntimeObservation{State: "ready", LaunchID: launchID}, nil
		})
	frame, err := ipc.NewConn(client).ReadFrame()
	if err != nil || frame.OK == nil || !*frame.OK {
		t.Fatal(frame, err)
	}
	if checks != 1 {
		t.Fatal("ready returned before startup was latched", checks)
	}
	selected = false // next operation has written pending
	if !d.startupReady() {
		t.Fatal("a new transition revoked an already ready owner's startup")
	}
	d.waitOwnerObservations()
}
