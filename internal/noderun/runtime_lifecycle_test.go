package noderun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type lifecycleTestAttempt struct {
	done   chan struct{}
	result error
}

func (a lifecycleTestAttempt) Done() <-chan struct{} { return a.done }
func (a lifecycleTestAttempt) Result() error         { return a.result }

// A second connection cannot launch during the same owner's unresolved attempt.
// This gate covers the interval before the worker obtains the journal lock.
func TestLifecycleEndpointKeepsGateUntilOwnedAttemptExits(t *testing.T) {
	store := ownerTestStore(t)
	done := make(chan struct{})
	defer close(done)
	var launches atomic.Int32
	d := New(store, nil).WithLifecycleLauncher(func(context.Context, runtimeowner.Command) (runtimeowner.Attempt, error) {
		launches.Add(1)
		return lifecycleTestAttempt{done: done}, nil
	})
	defer d.Stop()
	call := func(id string) ipc.Frame {
		a, b := net.Pipe()
		defer b.Close()
		b.SetDeadline(time.Now().Add(3 * time.Second))
		go d.serveIPC(ipc.NewConn(a))
		client := ipc.NewConn(b)
		params := map[string]string{"operation_id": id, "bundle": t.TempDir(), "namespace_sha256": lifecycleTestNamespace(t, store.Dir)}
		if err := ipc.Call(client, "request", "runtime.install", params); err != nil {
			t.Fatal(err)
		}
		frame, err := client.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	first := call(strings.Repeat("1", 32))
	if first.OK == nil || !*first.OK {
		t.Fatal(first)
	}
	var reply struct {
		OperationID string `json:"operation_id"`
		Accepted    bool   `json:"accepted"`
	}
	if json.Unmarshal(first.Result, &reply) != nil || !reply.Accepted {
		t.Fatal(first)
	}
	second := call(strings.Repeat("2", 32))
	if second.OK == nil || *second.OK || launches.Load() != 1 {
		t.Fatal("second lifecycle launch escaped gate", second, launches.Load())
	}
}

// A missing journal cannot justify another worker after parent-side image
// cleanup failed, either before Start or after the worker process exited.
func TestLifecycleGateRemainsClosedWhenLauncherCleanupIsUncertain(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-start", true: "after-exit"}[started], func(t *testing.T) {
			store := ownerTestStore(t)
			var launches atomic.Int32
			done := make(chan struct{})
			close(done)
			d := New(store, nil).WithLifecycleLauncher(func(context.Context, runtimeowner.Command) (runtimeowner.Attempt, error) {
				launches.Add(1)
				if started {
					return lifecycleTestAttempt{done: done, result: release.ErrCleanupUncertain}, nil
				}
				return nil, errors.Join(errors.New("image release"), release.ErrCleanupUncertain)
			})
			defer d.Stop()
			call := func(id string) {
				a, b := net.Pipe()
				defer b.Close()
				b.SetDeadline(time.Now().Add(3 * time.Second))
				go d.serveIPC(ipc.NewConn(a))
				client := ipc.NewConn(b)
				if err := ipc.Call(client, "install", "runtime.install", map[string]string{"operation_id": id, "bundle": t.TempDir(), "namespace_sha256": lifecycleTestNamespace(t, store.Dir)}); err != nil {
					t.Fatal(err)
				}
				if _, err := client.ReadFrame(); err != nil {
					t.Fatal(err)
				}
			}
			call(strings.Repeat("1", 32))
			d.waitLifecycleLaunches() // joins launcher; worker completion goroutine may still execute
			// Inspect via a fresh active daemon context so shutdown itself cannot
			// supply the refusal being tested; retain the same dispatch gate.
			second := New(store, nil)
			second.lifecycle = d.lifecycle
			defer second.Stop()
			time.Sleep(25 * time.Millisecond)
			a, b := net.Pipe()
			defer b.Close()
			b.SetDeadline(time.Now().Add(3 * time.Second))
			go second.serveIPC(ipc.NewConn(a))
			client := ipc.NewConn(b)
			ipc.Call(client, "next", "runtime.install", map[string]string{"operation_id": strings.Repeat("2", 32), "bundle": t.TempDir(), "namespace_sha256": lifecycleTestNamespace(t, store.Dir)})
			frame, err := client.ReadFrame()
			if err != nil || frame.OK == nil || *frame.OK || launches.Load() != 1 {
				t.Fatal("uncertain cleanup reopened launch gate", frame, err, launches.Load())
			}
		})
	}
}

// Shutdown cancels and joins the admission verifier without waiting for a worker
// that must remain alive across replacement of this serving process.
func TestLifecycleShutdownJoinsAdmission(t *testing.T) {
	store := ownerTestStore(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	d := New(store, nil).WithLifecycleLauncher(func(ctx context.Context, _ runtimeowner.Command) (runtimeowner.Attempt, error) {
		close(entered)
		<-ctx.Done()
		<-release
		return nil, ctx.Err()
	})
	a, b := net.Pipe()
	defer b.Close()
	go d.serveIPC(ipc.NewConn(a))
	client := ipc.NewConn(b)
	if err := ipc.Call(client, "start", "runtime.install", map[string]string{"operation_id": strings.Repeat("1", 32), "bundle": t.TempDir(), "namespace_sha256": lifecycleTestNamespace(t, store.Dir)}); err != nil {
		t.Fatal(err)
	}
	<-entered
	done := make(chan struct{})
	go func() { d.waitLifecycleLaunches(); close(done) }()
	select {
	case <-done:
		t.Fatal("shutdown returned before verifier exit")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join admission")
	}
}
