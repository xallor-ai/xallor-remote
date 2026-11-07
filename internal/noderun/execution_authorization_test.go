package noderun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/delegation"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// A first-lease refusal wins before executable resolution, job reservation or
// process startup. The trusted route ID, never a caller proof, is renewed.
func TestExecAuthorization_should_refuseBeforePreparingProcess_whenInitialLeaseDenied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frames := make(chan protocol.Message, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			var m protocol.Message
			if wsjson.Read(ctx, c, &m) != nil {
				return
			}
			frames <- m
		}
	}))
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetInbound(true); err != nil {
		t.Fatal(err)
	}
	d := New(s, nil)
	defer d.Stop()
	defer d.handoffs.Close()
	defer d.jobs.Close()
	link := &wsConn{ctx: ctx, conn: c}
	d.device = link
	scope := invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runLocalExec(ctx, link, protocol.Message{ExecID: "route", Scope: &scope, Payload: json.RawMessage(`{"program":{"executable":"must-not-resolve-program"}}`)})
	}()
	read := func() protocol.Message {
		t.Helper()
		select {
		case m := <-frames:
			return m
		case <-ctx.Done():
			t.Fatal("missing frame")
			return protocol.Message{}
		}
	}
	request := read()
	if request.Type != protocol.TypeExecutionRenewRoute || request.ExecID != "route" || request.Delegation != "" {
		t.Fatal(request)
	}
	d.onDeviceMsg(link, protocol.Message{Type: protocol.TypeExecutionLease, RequestID: request.RequestID, Code: protocol.Unauthorized})
	if m := read(); m.Type != protocol.TypeInvokeNack || m.Code != protocol.Unauthorized {
		t.Fatal(m)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("exec did not stop")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "jobs.json")); !os.IsNotExist(err) {
		t.Fatal("denied invocation reserved a job", err)
	}
}

// Pre-start authority failure is distinct from caller cancellation, timeout,
// and a relay failure; expiry after authorization also cannot become agent_error.
func TestExecAuthorization_should_classifyInitialFailure_withoutInventingTargetFailure(t *testing.T) {
	if got := executionLeaseStartCode(context.Background(), delegation.ErrDenied); got != protocol.Unauthorized {
		t.Fatal(got)
	}
	for _, err := range []error{delegation.ErrExpired, context.DeadlineExceeded, errors.New("offline")} {
		if got := executionLeaseStartCode(context.Background(), err); got != protocol.RelayError {
			t.Fatal(got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := executionLeaseStartCode(ctx, delegation.ErrDenied); got != protocol.Cancelled {
		t.Fatal(got)
	}
	ctx, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if got := executionLeaseStartCode(ctx, delegation.ErrDenied); got != protocol.ExecTimeout {
		t.Fatal(got)
	}
	ctx, cause := context.WithCancelCause(context.Background())
	cause(delegation.ErrExpired)
	if got := startFailureCode(ctx); got != protocol.Unauthorized {
		t.Fatal(got)
	}
}

// Revocation racing process startup cannot hide a possible started process or
// an unconfirmed capacity release behind a never-executed rejection.
func TestExecAuthorization_should_preserveStartupUncertainty_evenWhenAuthorityLost(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(delegation.ErrDenied)
	for _, tc := range []struct {
		started              bool
		startErr, releaseErr error
	}{
		{true, errors.New("attach failed"), nil},
		{false, process.ErrCleanup, nil},
		{false, context.Canceled, errors.New("release failed")},
	} {
		if m := failedStartMessage(ctx, "id", tc.started, tc.startErr, tc.releaseErr); m.Type != protocol.TypeError || m.Code != protocol.AgentError {
			t.Fatal(m)
		}
	}
	if m := failedStartMessage(ctx, "id", false, context.Canceled, nil); m.Type != protocol.TypeInvokeNack || m.Code != protocol.Unauthorized {
		t.Fatal(m)
	}
}
