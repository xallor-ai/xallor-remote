package runtimeowner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

type workerTestLease struct{ checkErr, closeErr error }

func (l workerTestLease) Check() error { return l.checkErr }
func (l workerTestLease) Close() error { return l.closeErr }

func workerEnvelope(t *testing.T) Handoff {
	t.Helper()
	command, err := NewCommand("install", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, _ := command.Request()
	return Handoff{Command: command, Permit: release.OwnerHandoff{OperationID: r.ID, IntentSHA256: r.IntentSHA256}}
}

// Authentication denial cannot create a journal or run a version/stop/start probe.
func TestWorkerAuthenticatesBeforeAdmission(t *testing.T) {
	j := journal(t)
	control := &untouchedControl{}
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: control,
		Authenticate: func(context.Context, Handoff) (Admission, error) { return nil, errors.New("unverified parent") },
		Listen:       func() (net.Listener, error) { t.Fatal("listened before authentication"); return nil, nil }}
	if err := w.Run(context.Background(), bytes.NewReader(raw), io.Discard); err == nil {
		t.Fatal("authentication denial lost")
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) || control.calls != 0 {
		t.Fatal("denied handoff had effects", err, control.calls)
	}
}

// Failure to release admission proof is uncertainty even with no release effects.
func TestWorkerDoesNotRunAfterAdmissionResourceReleaseFailure(t *testing.T) {
	j := journal(t)
	control := &untouchedControl{}
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: control,
		Authenticate: func(context.Context, Handoff) (Admission, error) {
			return workerTestLease{closeErr: errors.New("observer release failed")}, nil
		},
		Listen: func() (net.Listener, error) { t.Fatal("listened after failed observer cleanup"); return nil, nil }}
	if err := w.Run(context.Background(), bytes.NewReader(raw), io.Discard); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	r, err := j.Query(h.Command.ID)
	if err != nil || r.Outcome != Uncertain || control.calls != 0 {
		t.Fatal(r, err, control.calls)
	}
}

type workerChangedLease struct{ checks int }

func (l *workerChangedLease) Check() error {
	l.checks++
	if l.checks > 1 {
		return errors.New("parent changed before admission")
	}
	return nil
}
func (l *workerChangedLease) Close() error { return errors.New("admission resource release failed") }

// Refusal after the receipt is created must include admission-resource cleanup
// before choosing its outcome; deferred Close cannot leave a false Rejected.
func TestWorkerRetainsUncertaintyWhenPostAdmissionRefusalCleanupFails(t *testing.T) {
	j := journal(t)
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: &untouchedControl{},
		Authenticate: func(context.Context, Handoff) (Admission, error) { return &workerChangedLease{}, nil },
		Listen:       func() (net.Listener, error) { t.Fatal("refused admission opened endpoint"); return nil, nil }}
	if err := w.Run(context.Background(), bytes.NewReader(raw), io.Discard); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	r, err := j.Query(h.Command.ID)
	if err != nil || r.Outcome != Uncertain {
		t.Fatal(r, err)
	}
}

// Acceptance is emitted only after durable admission and query availability.
// A deterministic missing-source failure still becomes an observable rejection.
func TestWorkerPublishesAcceptanceThenRecordsBackendResult(t *testing.T) {
	j := journal(t)
	control := &untouchedControl{}
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: control,
		Authenticate: func(context.Context, Handoff) (Admission, error) { return workerTestLease{}, nil },
		Listen: func() (net.Listener, error) {
			if _, err := j.Active(); err != nil {
				t.Fatal("listener preceded durable admission", err)
			}
			return net.Listen("tcp", "127.0.0.1:0")
		}}
	var ack bytes.Buffer
	if err := w.Run(context.Background(), bytes.NewReader(raw), &ack); err == nil {
		t.Fatal("missing source accepted")
	}
	var acceptance Acceptance
	if err := json.Unmarshal(ack.Bytes(), &acceptance); err != nil {
		t.Fatal(err)
	}
	if !acceptance.Accepted || acceptance.OperationID != h.Command.ID || acceptance.PID != os.Getpid() {
		t.Fatal(acceptance)
	}
	r, err := j.Query(h.Command.ID)
	if err != nil || r.Outcome != Rejected || control.calls != 0 {
		t.Fatal(r, err, control.calls)
	}
}
