package relay

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type repeatPendingSender struct {
	*recSender
	pending atomic.Int32
	approve func()
}

func (s *repeatPendingSender) Enqueue(m protocol.Message) error {
	if m.Type == protocol.TypeSessionResult && m.Status == protocol.SessionPending && s.pending.Add(1) == 2 {
		go s.approve()
		runtime.Gosched()
	}
	return s.recSender.Enqueue(m)
}

// Retrying an identical pending request must preserve pending-before-terminal
// order even when approval races the duplicate reply rather than initial issue.
func TestSessionRequest_shouldKeepMonotonicResults_whenPendingRetryRacesApproval(t *testing.T) {
	s, h, target, _ := approvedSessionFixture(t)
	client := &repeatPendingSender{recSender: &recSender{}}
	request := nextSessionRequest(t, s, h, client, "retry", 2)
	h.Handle(client, protocol.RoleClient, "device", request)
	approval := target.last()
	done := make(chan struct{})
	client.approve = func() {
		defer close(done)
		h.Handle(target, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: approval.RequestID, SessionID: approval.SessionID, RequestSHA256: approval.RequestSHA256, Allow: protocol.Bool(true)})
	}
	h.Handle(client, protocol.RoleClient, "device", request)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("approval did not finish")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	terminal := false
	for _, m := range client.got {
		if m.Type != protocol.TypeSessionResult {
			continue
		}
		if m.Status == protocol.SessionPending && terminal {
			t.Error("pending followed terminal")
		}
		if m.Status == protocol.SessionActive {
			terminal = true
		}
	}
	if !terminal {
		t.Error("no approved terminal")
	}
}

type committedReservationErrorStore struct {
	accesscode.Store
	calls int
}

func (s *committedReservationErrorStore) WriteAccessCode(ctx context.Context, fn func(accesscode.Transaction) error) error {
	s.calls++
	if err := s.Store.WriteAccessCode(ctx, fn); err != nil {
		return err
	}
	return errors.New("fixture lost commit acknowledgement")
}

// A committed reservation can lose its acknowledgement. Without proof of
// rollback, report unknown and never replay the write or send recipient approval.
func TestSessionRequest_shouldReportUnknown_whenReservationCommitAcknowledgementIsLost(t *testing.T) {
	s, h, _, _ := approvedSessionFixture(t)
	client := &recSender{}
	request := nextSessionRequest(t, s, h, client, "uncertain", 2)
	failed := &committedReservationErrorStore{Store: s.SQLStore}
	h.accessCodes = failed
	h.Handle(client, protocol.RoleClient, "device", request)
	got := client.last()
	if got.Status != protocol.SessionUnknownState || got.Code != protocol.SessionUnknown || failed.calls != 1 {
		t.Fatalf("uncertain reserve misreported/replayed: %s %s calls=%d", got.Status, got.Code, failed.calls)
	}
	if h.pendingSessions[request.SessionID] != nil {
		t.Fatal("uncertain reserve published approval")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		record, err := r.Reservation(context.Background(), request.RequestID)
		if err != nil {
			return err
		}
		if record.State != accesscode.ReservationReserved {
			t.Error("committed reservation missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func nextSessionRequest(t *testing.T, s *relaySQLFixture, h *Hub, client Sender, id string, generation uint64, scope ...string) protocol.Message {
	t.Helper()
	if _, exists := h.clients[client]; !exists {
		if code := h.RegisterClient("device", "xr_grant_transport", "", client); code != "" {
			t.Fatal(code)
		}
	}
	if len(scope) == 0 {
		scope = []string{protocol.OpInfo}
	}
	now := time.Now().UTC()
	code := accesscode.Code{ID: "code-" + id, DeviceUID: h.devices["device"].record.UID, Type: accesscode.OneTime, Generation: generation, IssuedAt: now, Scope: scope, RequireApproval: true, State: accesscode.StateActive}
	if err := s.WriteAccessCode(context.Background(), func(tx accesscode.Transaction) error {
		return tx.InsertCode(context.Background(), code, protocol.SHA256Hex("secret"))
	}); err != nil {
		t.Fatal(err)
	}
	return protocol.Message{Type: protocol.TypeSessionRequest, RequestID: "request-" + id, SessionID: "session-" + id, TargetUID: code.DeviceUID, CodeID: code.ID, CodeGeneration: generation, SessionScope: code.Scope, Grant: "secret"}
}

// Both reservations can commit before either request publishes pending. The
// second publication must not overwrite the winner or strand its controller.
func TestSessionRequest_shouldKeepExistingPending_whenReservationsRaceForSessionID(t *testing.T) {
	s, h, _, _ := approvedSessionFixture(t)
	a, b := &recSender{}, &recSender{}
	ma := nextSessionRequest(t, s, h, a, "a", 2)
	mb := nextSessionRequest(t, s, h, b, "b", 3)
	mb.SessionID = ma.SessionID
	barrier := &resumeCommitBarrier{Store: s.SQLStore, entered: make(chan struct{}), release: make(chan struct{})}
	h.accessCodes = barrier
	var once sync.Once
	unpause := func() { once.Do(func() { close(barrier.release) }) }
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(a, protocol.RoleClient, "device", ma) }()
	defer func() { unpause(); <-done }()
	select {
	case <-barrier.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reservation barrier not reached")
	}
	h.Handle(b, protocol.RoleClient, "device", mb)
	winner := h.pendingSessions[ma.SessionID]
	if winner == nil || winner.client != b {
		t.Fatal("second request did not establish pending")
	}
	unpause()
	<-done
	if h.pendingSessions[ma.SessionID] != winner || !h.clients[b].sessionRequired || h.clients[a].sessionRequired {
		t.Error("pending winner overwritten or controller stranded")
	}
	if err := s.ReadAccessCode(context.Background(), func(r accesscode.Reader) error {
		loser, err := r.Reservation(context.Background(), ma.RequestID)
		if err != nil {
			return err
		}
		if loser.State != accesscode.ReservationReleased {
			t.Errorf("unpublished reservation retained: %s", loser.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type immediateApprovalSender struct {
	*recSender
	h    *Hub
	done chan struct{}
}

func (s *immediateApprovalSender) Enqueue(m protocol.Message) error {
	err := s.recSender.Enqueue(m)
	if m.Type == protocol.TypeSessionApprovalRequest {
		go func() {
			defer close(s.done)
			s.h.Handle(s, protocol.RoleDevice, "device", protocol.Message{Type: protocol.TypeSessionApprovalResult, RequestID: m.RequestID, SessionID: m.SessionID, RequestSHA256: m.RequestSHA256, Allow: protocol.Bool(true)})
		}()
	}
	return err
}

type yieldPendingSender struct{ *recSender }

func (s *yieldPendingSender) Enqueue(m protocol.Message) error {
	if m.Type == protocol.TypeSessionResult && m.Status == protocol.SessionPending {
		runtime.Gosched()
	}
	return s.recSender.Enqueue(m)
}

// An immediately approving target races pending delivery. Yield only to the
// scheduler (no waiting for peer/I/O in Enqueue), and require monotonic results.
func TestSessionRequest_shouldQueuePendingBeforeTerminal_whenTargetApprovesImmediately(t *testing.T) {
	for i := 0; i < 20; i++ {
		s, h, target, _ := approvedSessionFixture(t)
		immediate := &immediateApprovalSender{recSender: target, h: h, done: make(chan struct{})}
		h.devices["device"].conn = immediate
		h.devices["device"].sessionConnection = immediate
		client := &yieldPendingSender{recSender: &recSender{}}
		request := nextSessionRequest(t, s, h, client, "immediate", 2)
		h.Handle(client, protocol.RoleClient, "device", request)
		select {
		case <-immediate.done:
		case <-time.After(3 * time.Second):
			t.Fatal("approval did not finish")
		}
		client.mu.Lock()
		messages := append([]protocol.Message(nil), client.got...)
		client.mu.Unlock()
		states := []string{}
		for _, m := range messages {
			if m.Type == protocol.TypeSessionResult {
				states = append(states, m.Status)
			}
		}
		if len(states) != 2 || states[0] != protocol.SessionPending || states[1] != protocol.SessionActive {
			t.Fatalf("reversed lifecycle: %v", states)
		}
	}
}
