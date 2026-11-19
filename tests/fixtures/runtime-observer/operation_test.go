package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// Live queries must bind the native transaction peer and exact command intent;
// a correct operation ID alone cannot authorize a different installation input.
func TestOperation_shouldBindNativePeerAndExactIntent(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-peer", "wrong-intent", "wrong-namespace"} {
		t.Run(mode, func(t *testing.T) {
			profile, err := os.MkdirTemp("", "xr-op-observer-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(profile); err != nil {
					t.Error(err)
				}
			})
			t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
			bundle := filepath.Join(profile, "bundle")
			id := strings.Repeat("a", 32)
			request, _ := (runtimeowner.Command{ID: id, Kind: "install", Bundle: bundle}).Request()
			journal := runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
			namespace, _ := journal.NamespaceID()
			if mode == "wrong-intent" {
				request.IntentSHA256 = strings.Repeat("b", 64)
			}
			if mode == "wrong-namespace" {
				namespace = strings.Repeat("c", 64)
			}
			record := runtimeowner.Record{Request: request, NamespaceSHA256: namespace, Outcome: runtimeowner.Pending, Effects: []runtimeowner.Effect{}}
			listener, err := ipc.ListenLifecycle()
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				conn := ipc.NewConn(raw)
				req, err := conn.ReadRequest()
				if err != nil {
					done <- err
					return
				}
				pid := os.Getpid()
				if mode == "wrong-peer" {
					pid++
				}
				observer := buildinfo.Executable{Info: buildinfo.Info{Version: buildinfo.Version(), Revision: strings.Repeat("d", 40), Protocol: 1, ProfileFormat: 1}, PID: pid, Path: "test-observer", SHA256: strings.Repeat("e", 64), OwnerProtocol: 1}
				done <- conn.WriteFrame(ipc.OK(req.ID, map[string]any{"operation": record, "settled": false, "observer": observer}))
			}()
			observed, err := observeOperation(profile, id, bundle, false)
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
			if mode != "valid" {
				if err == nil {
					t.Fatal("invalid operation observation accepted")
				}
				return
			}
			if err != nil || observed.Source != "lifecycle_ipc" || observed.PeerPID != os.Getpid() {
				t.Fatal("live operation not bound", err)
			}
		})
	}
}

// An offline query exposes no live peer/observer and never grants lifecycle
// execution. Its result must still belong to the exact requested input/profile.
func TestOperation_shouldKeepJournalObservationSeparateFromLivePeer(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	id := strings.Repeat("a", 32)
	bundle := filepath.Join(profile, "bundle")
	request, _ := (runtimeowner.Command{ID: id, Kind: "install", Bundle: bundle}).Request()
	journal := runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
	tx, err := journal.Admit(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	view, err := observeOperation(profile, id, bundle, true)
	if err != nil || view.Source != "journal_read_only" || view.PeerPID != 0 || view.Observer != nil {
		t.Fatal("journal masqueraded as live peer", err)
	}
	if _, err := observeOperation(profile, id, filepath.Join(profile, "other"), true); err == nil {
		t.Fatal("wrong input intent accepted")
	}
}
