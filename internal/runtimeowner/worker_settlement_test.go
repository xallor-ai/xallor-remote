package runtimeowner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

type settlementListener struct {
	net.Listener
	closes      atomic.Int32
	beforeClose func()
	closeErr    error
}

type attributedSettlementLease struct {
	check    func() error
	closes   int
	closeErr error
}

func (l *attributedSettlementLease) Check() error { return l.check() }
func (l *attributedSettlementLease) Close() error { l.closes++; return l.closeErr }

// The retained authentication proof is attributed immediately after admission,
// before rechecking; failed release prevents listener and backend allocation.
func TestWorkerShouldAttributeAdmissionProofBeforePostAdmissionCheck(t *testing.T) {
	j := journal(t)
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	checks := 0
	lease := &attributedSettlementLease{closeErr: errors.New("proof close failure")}
	lease.check = func() error {
		checks++
		if checks == 2 {
			rows, e := j.Resources(h.Command.ID)
			if e != nil || len(rows) != 1 || rows[0].Phase != ResourcePhaseWorker || rows[0].Status != ResourceAllocated {
				t.Errorf("unattributed admitted proof: %v %v", rows, e)
			}
		}
		return nil
	}
	control := &untouchedControl{}
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: control, Authenticate: func(context.Context, Handoff) (Admission, error) { return lease, nil }, Listen: func() (net.Listener, error) { t.Error("listen after failed proof release"); return nil, ErrInvalid }}
	err := w.Run(context.Background(), bytes.NewReader(raw), io.Discard)
	rows, e := j.Resources(h.Command.ID)
	if !errors.Is(err, ErrUncertain) || e != nil || len(rows) != 1 || rows[0].Status != ResourceUnknown || lease.closes != 1 || control.calls != 0 {
		t.Fatal(err, rows, e, lease.closes, control.calls)
	}
}

type lostSettlementACK struct {
	cancel context.CancelFunc
	err    error
}

func (w lostSettlementACK) Write(p []byte) (int, error) { w.cancel(); return 0, w.err }

// Losing the acknowledgement and admission context still executes the original
// missing-source request and closes its worker groups without replay.
func TestWorkerShouldExecuteAfterLostAcknowledgementAndCancellation(t *testing.T) {
	j := journal(t)
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ackErr := errors.New("lost acknowledgement")
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: &untouchedControl{}, Authenticate: func(context.Context, Handoff) (Admission, error) { return workerTestLease{}, nil }, Listen: func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }}
	err := w.Run(ctx, bytes.NewReader(raw), lostSettlementACK{cancel: cancel, err: ackErr})
	if !errors.Is(err, ackErr) || !errors.Is(err, fs.ErrNotExist) || errors.Is(err, context.Canceled) {
		t.Fatal("original backend not executed after delivery loss", err)
	}
	r, settled, e := j.QuerySettled(h.Command.ID)
	if e != nil || !settled || r.Outcome != Rejected {
		t.Fatal(r, settled, e)
	}
}

// A failed listener factory can still transfer a live socket to the worker.
func TestWorkerShouldClosePartialListenerAndRetainFailure(t *testing.T) {
	j := journal(t)
	h := workerEnvelope(t)
	raw, _ := EncodeHandoff(h)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &settlementListener{Listener: ln, closeErr: errors.New("partial close failure")}
	w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: &untouchedControl{}, Authenticate: func(context.Context, Handoff) (Admission, error) { return workerTestLease{}, nil }, Listen: func() (net.Listener, error) {
		rows, e := j.Resources(h.Command.ID)
		if e != nil || len(rows) != 2 {
			t.Errorf("query allocation preceded intent: %v %v", rows, e)
		}
		return wrapped, errors.New("partial listen failure")
	}}
	err = w.Run(context.Background(), bytes.NewReader(raw), io.Discard)
	if !errors.Is(err, ErrUncertain) || wrapped.closes.Load() != 1 {
		t.Fatal(err, wrapped.closes.Load())
	}
	rows, e := j.Resources(h.Command.ID)
	if e != nil {
		t.Fatal(e)
	}
	unknown := 0
	for _, row := range rows {
		if row.Status == ResourceUnknown {
			unknown++
		}
	}
	if unknown != 1 {
		t.Fatal(rows)
	}
}

type settlementConn struct {
	net.Conn
	closes   atomic.Int32
	closed   chan struct{}
	closeErr error
}

func (c *settlementConn) Close() error {
	n := c.closes.Add(1)
	err := c.Conn.Close()
	if n == 1 {
		close(c.closed)
	}
	return errors.Join(err, c.closeErr)
}

type acceptingSettlementListener struct {
	net.Listener
	accepted chan *settlementConn
}

func (l *acceptingSettlementListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return c, e
	}
	wrapped := &settlementConn{Conn: c, closed: make(chan struct{}), closeErr: errors.New("connection close failure")}
	l.accepted <- wrapped
	return wrapped, nil
}

// Idle accepted sockets and a rejected overload socket close exactly once, and
// the server joins all readers before returning their injected cleanup failure.
func TestQueriesShouldRetainAcceptedAndOverflowConnectionCloseFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &acceptingSettlementListener{Listener: ln, accepted: make(chan *settlementConn, 9)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeQueries(ctx, listener, journal(t)) }()
	var owned []*settlementConn
	for i := 0; i < 9; i++ {
		c, e := net.Dial("tcp", ln.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		select {
		case accepted := <-listener.accepted:
			owned = append(owned, accepted)
		case <-time.After(time.Second):
			t.Fatal("accept stalled")
		}
	}
	select {
	case <-owned[8].closed:
	case <-time.After(time.Second):
		t.Fatal("overflow not closed")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, release.ErrCleanupUncertain) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("query shutdown did not join")
	}
	for _, c := range owned {
		if c.closes.Load() != 1 {
			t.Fatal("close count", c.closes.Load())
		}
	}
}

// Reentrant execution during settlement cannot finish the pending original.
func TestRunShouldKeepCompletionOwnedThroughSettlement(t *testing.T) {
	j := journal(t)
	h := workerEnvelope(t)
	req, _ := h.Command.Request()
	tx, e := j.Admit(req)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Close()
	err := tx.runWithSettlement(h.Command, release.Store{Dir: filepath.Dir(j.Dir)}, &untouchedControl{}, func() error {
		if e := tx.Run(Command{}, release.Store{}, &untouchedControl{}); !errors.Is(e, ErrRecorded) {
			t.Error(e)
		}
		if e := tx.Complete(nil); !errors.Is(e, ErrRecorded) {
			t.Error(e)
		}
		r, e := j.Query(req.ID)
		if e != nil || r.Outcome != Pending {
			t.Error(r, e)
		}
		return release.ErrCleanupUncertain
	})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
}

func (l *settlementListener) Close() error {
	l.closes.Add(1)
	if l.beforeClose != nil {
		l.beforeClose()
	}
	return errors.Join(l.Listener.Close(), l.closeErr)
}

// Real socket closure must precede the terminal write, and its error remains debt.
func TestWorkerShouldSettleQueryClosureBeforeTerminalOutcome(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "uncertain"}[fail], func(t *testing.T) {
			j := journal(t)
			h := workerEnvelope(t)
			raw, _ := EncodeHandoff(h)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &settlementListener{Listener: ln}
			wrapped.beforeClose = func() {
				r, e := j.Query(h.Command.ID)
				if e != nil || r.Outcome != Pending {
					t.Errorf("terminal before query shutdown: %+v %v", r, e)
				}
			}
			if fail {
				wrapped.closeErr = errors.New("injected listener close failure")
			}
			w := Worker{Journal: j, Store: release.Store{Dir: filepath.Dir(j.Dir)}, Control: &untouchedControl{}, Authenticate: func(context.Context, Handoff) (Admission, error) { return workerTestLease{}, nil }, Listen: func() (net.Listener, error) { return wrapped, nil }}
			err = w.Run(context.Background(), bytes.NewReader(raw), io.Discard)
			r, e := j.Query(h.Command.ID)
			if e != nil {
				t.Fatal(e)
			}
			want := Rejected
			if fail {
				want = Uncertain
				if !errors.Is(err, ErrUncertain) {
					t.Error("missing uncertainty", err)
				}
			}
			if r.Outcome != want || wrapped.closes.Load() != 1 {
				t.Fatalf("outcome=%s closes=%d", r.Outcome, wrapped.closes.Load())
			}
			rows, e := j.Resources(h.Command.ID)
			if e != nil {
				t.Fatal(e)
			}
			workerRows := 0
			for _, row := range rows {
				if row.Phase == ResourcePhaseWorker {
					workerRows++
					if !fail && row.Status != ResourceClosed {
						t.Error(row)
					}
				}
			}
			if workerRows != 2 {
				t.Fatalf("worker groups=%d", workerRows)
			}
		})
	}
}
