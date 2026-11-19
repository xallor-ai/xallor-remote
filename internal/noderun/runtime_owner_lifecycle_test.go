package noderun

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Cancellation begins cleanup; it does not prove the external verifier has
// exited. Shutdown must wait for admitted observation to actually return.
func TestOwnerObservation_should_waitForVerifierCleanup_onShutdown(t *testing.T) {
	d := New(ownerTestStore(t), nil)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	started, cancelled, releaseVerifier := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseWorker := func() { once.Do(func() { close(releaseVerifier) }) }
	defer releaseWorker()
	d.startOwnerObservation(ipc.NewConn(a), ipc.Request{ID: "observe", Method: "runtime.status"}, func(ctx context.Context) (release.RuntimeObservation, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-releaseVerifier
		return release.RuntimeObservation{State: "unknown"}, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("observer not started")
	}
	done := make(chan struct{})
	go func() { d.waitOwnerObservations(); close(done) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("verifier not cancelled")
	}
	select {
	case <-done:
		t.Fatal("shutdown returned while verifier still cleaning up")
	case <-time.After(40 * time.Millisecond):
	}
	releaseWorker()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not complete after verifier returned")
	}
}

// A head that never reads its result must not strand a verifier worker during
// daemon shutdown. Closing the transport unblocks the actual net.Pipe write.
func TestOwnerObservation_should_unblockUnreadResponse_onShutdown(t *testing.T) {
	d := New(ownerTestStore(t), nil)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	returned := make(chan struct{})
	d.startOwnerObservation(ipc.NewConn(a), ipc.Request{ID: "unread", Method: "runtime.status"}, func(context.Context) (release.RuntimeObservation, error) {
		close(returned)
		return release.RuntimeObservation{State: "unverified"}, nil
	})
	<-returned
	done := make(chan struct{})
	go func() { d.waitOwnerObservations(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unread IPC reply blocked shutdown")
	}
}

// Requests queued after shutdown must not launch a new source verifier.
func TestOwnerObservation_should_rejectAdmission_afterShutdown(t *testing.T) {
	d := New(ownerTestStore(t), nil)
	d.waitOwnerObservations()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var calls atomic.Int32
	d.startOwnerObservation(ipc.NewConn(a), ipc.Request{ID: "late", Method: "runtime.status"}, func(context.Context) (release.RuntimeObservation, error) {
		calls.Add(1)
		return release.RuntimeObservation{}, nil
	})
	_ = b.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = ipc.NewConn(b).ReadFrame()
	if calls.Load() != 0 {
		t.Fatal("verification launched after shutdown")
	}
}
