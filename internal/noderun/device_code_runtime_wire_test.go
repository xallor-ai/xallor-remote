package noderun

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/account"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/principal"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/relay"
	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// Committed code mutations must reach the caller as their typed result, without
// closing the authenticated device link or being parsed as ownership state.
// Daemon/Relay share a process; TLS, SQLite and net.Pipe IPC framing are real.
func TestDeviceCodeRuntime_shouldReturnCommittedResult_overTLSAndIPC(t *testing.T) {
	for _, action := range []string{"code.revoke", "code.rotate"} {
		t.Run(action, func(t *testing.T) {
			d, s, device := newDeviceCodeRuntime(t)
			ctx := context.Background()
			code := accesscode.Code{ID: "managed-code", DeviceUID: device.UID, OwnershipID: device.OwnershipID,
				Type: accesscode.LongLived, Generation: 1, IssuedAt: time.Now().UTC(),
				Scope: []string{protocol.OpInfo}, RequireApproval: true, State: accesscode.StateActive}
			if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
				return tx.InsertCode(ctx, code, protocol.SHA256Hex("old-code-secret"))
			}); err != nil {
				t.Fatal(err)
			}
			req := access.Request{Action: action, CodeID: code.ID}
			if action == "code.rotate" {
				req.NewCodeID, req.NewSecret = "replacement-code", "replacement-secret"
			}
			result := deviceAccessIPC(t, d, req)
			var persisted accesscode.Code
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				var err error
				persisted, err = r.Code(ctx, code.ID)
				return err
			}); err != nil || persisted.State != accesscode.StateRevoked {
				t.Fatalf("mutation did not commit: state=%s err=%v", persisted.State, err)
			}
			if result.OK == nil || !*result.OK {
				t.Fatalf("database committed but IPC did not acknowledge %s: code=%s", action, result.Code)
			}
			var value struct {
				CodeID     string   `json:"code_id"`
				State      string   `json:"state"`
				Generation uint64   `json:"generation"`
				Type       string   `json:"type"`
				Scope      []string `json:"scope"`
				Secret     string   `json:"secret"`
			}
			if err := json.Unmarshal(result.Result, &value); err != nil {
				t.Fatal(err)
			}
			if action == "code.revoke" {
				if value.CodeID != "managed-code" || value.State != "revoked" || value.Secret != "" {
					t.Fatal("incorrect revoke acknowledgement")
				}
			} else {
				var rotated access.CodeRotationResult
				if json.Unmarshal(result.Result, &rotated) != nil || rotated.PreviousCodeID != "managed-code" || rotated.Code.ID != "replacement-code" || rotated.Code.Generation != 2 ||
					rotated.Code.Type != accesscode.LongLived || rotated.Code.State != accesscode.StateActive || len(rotated.Code.Scope) != 1 || rotated.Code.Scope[0] != protocol.OpInfo || value.Secret != "" {
					t.Fatal("incorrect rotation acknowledgement")
				}
			}
			status := deviceAccessIPC(t, d, access.Request{Action: "status"})
			if status.OK == nil || !*status.OK {
				t.Fatalf("management link lost after mutation: %s", status.Code)
			}
		})
	}
}

func deviceAccessIPC(t *testing.T, d *Daemon, req access.Request) ipc.Frame {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	_ = a.SetDeadline(time.Now().Add(10 * time.Second))
	_ = b.SetDeadline(time.Now().Add(10 * time.Second))
	handled := make(chan bool, 1)
	go func() {
		handled <- d.handleDeviceAccess(ipc.NewConn(a), ipc.Request{ID: "manage", Method: "device.access", Params: mustJSON(req)})
	}()
	frame, err := ipc.NewConn(b).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !<-handled || frame.ID != "manage" {
		t.Fatal("management IPC correlation failed")
	}
	return frame
}

func newDeviceCodeRuntime(t *testing.T) (*Daemon, *storage.SQLStore, access.Device) {
	t.Helper()
	s, err := storage.OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	owner, err := account.New(s).CreatePrincipal(context.Background(), account.CreatePrincipalRequest{Name: "code-owner", Kind: "user", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	hub := relay.NewHub(s, principal.NewService(s), access.NewService(s), nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}), s)
	server := httptest.NewTLSServer(relay.Handler(hub, nil))
	t.Cleanup(server.Close)
	prior := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = prior })
	local, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := local.SetConfig("wss"+strings.TrimPrefix(server.URL, "https"), t.TempDir(), "auto"); err != nil {
		t.Fatal(err)
	}
	if err := local.SetAccount(owner.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := local.IssueGrant(); err != nil {
		t.Fatal(err)
	}
	if _, err := local.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	d := New(local, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	go func() { done <- d.connectDevice() }()
	t.Cleanup(func() {
		d.Stop()
		d.waitLocalExecs()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("device connection did not stop")
		}
		d.handoffs.Close()
		d.jobs.Close()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.mu.Lock()
		ready := d.device != nil
		d.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("device connection not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	claim := deviceAccessIPC(t, d, access.Request{Action: "claim"})
	if claim.OK == nil || !*claim.OK {
		t.Fatalf("owner claim failed: %s", claim.Code)
	}
	device, err := s.LoadDevice(context.Background(), local.ID())
	if err != nil {
		t.Fatal(err)
	}
	return d, s, device
}
