package noderun

import (
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func subscribeReplyForTest(t *testing.T, cl *clientLink, kind, request, session string) <-chan protocol.Message {
	t.Helper()
	w, err := cl.subscribeSessionReply(request, session, kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.unsubscribeSessionReply(request, w) })
	return w.ch
}

// Two callers may name the same session; reversed terminal arrival must never
// deliver one request's rotated capability or error to the other IPC caller.
func TestSessionReplies_shouldCorrelateRequest_whenSessionIsShared(t *testing.T) {
	for _, kind := range []string{protocol.TypeSessionResult, protocol.TypeSessionResumeResult} {
		t.Run(kind, func(t *testing.T) {
			cl := &clientLink{}
			a := subscribeReplyForTest(t, cl, kind, "request-a", "same-session")
			b := subscribeReplyForTest(t, cl, kind, "request-b", "same-session")
			cl.dispatch(protocol.Message{Type: kind, RequestID: "request-b", SessionID: "same-session", Status: protocol.SessionRejectedState, Code: protocol.Unauthorized})
			cl.dispatch(protocol.Message{Type: kind, RequestID: "request-a", SessionID: "same-session", Status: protocol.SessionActive, ResumeCapability: "only-a"})
			for _, expect := range []struct {
				ch             <-chan protocol.Message
				id, capability string
			}{{a, "request-a", "only-a"}, {b, "request-b", ""}} {
				select {
				case got := <-expect.ch:
					if got.RequestID != expect.id || got.ResumeCapability != expect.capability {
						t.Errorf("reply crossed request boundary: got request=%s", got.RequestID)
					}
				default:
					t.Errorf("terminal lost for %s", expect.id)
				}
			}
		})
	}
}

// A stale request's result with the same session cannot settle a newer request.
// Ignoring progress duplicates must leave room for the matching terminal.
func TestSessionReplies_shouldIgnoreUnrelatedAndProgress_whenWaitingForTerminal(t *testing.T) {
	for _, kind := range []string{protocol.TypeSessionResult, protocol.TypeSessionResumeResult} {
		t.Run(kind, func(t *testing.T) {
			cl := &clientLink{}
			ch := subscribeReplyForTest(t, cl, kind, "expected", "session")
			otherKind := protocol.TypeSessionResult
			if kind == otherKind {
				otherKind = protocol.TypeSessionResumeResult
			}
			cl.dispatch(protocol.Message{Type: otherKind, RequestID: "expected", SessionID: "session", Status: protocol.SessionActive})
			cl.dispatch(protocol.Message{Type: kind, RequestID: "stale", SessionID: "session", Status: protocol.SessionActive})
			cl.dispatch(protocol.Message{Type: kind, RequestID: "expected", SessionID: "wrong-session", Status: protocol.SessionActive})
			for i := 0; i < 12; i++ {
				cl.dispatch(protocol.Message{Type: kind, RequestID: "expected", SessionID: "session", Status: protocol.SessionPending})
			}
			cl.dispatch(protocol.Message{Type: kind, RequestID: "expected", SessionID: "session", Status: protocol.SessionActive, ResumeID: "rotated"})
			cl.failWaiters()
			cl.failWaiters()
			select {
			case got := <-ch:
				if got.RequestID != "expected" || got.Status != protocol.SessionActive || got.ResumeID != "rotated" {
					t.Errorf("matching terminal lost or replaced: request=%s status=%s", got.RequestID, got.Status)
				}
			default:
				t.Error("matching terminal was dropped")
			}
			select {
			case <-ch:
				t.Error("more than one terminal delivered")
			default:
			}
		})
	}
}

// Registration cannot overwrite an existing waiter or start after link loss;
// delayed cleanup from an earlier waiter cannot remove a newer registration.
func TestSessionReplies_shouldProtectRegistration_whenDuplicateOrClosed(t *testing.T) {
	cl := &clientLink{}
	w, err := cl.subscribeSessionReply("id", "session", protocol.TypeSessionResult)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.subscribeSessionReply("id", "other", protocol.TypeSessionResumeResult); err == nil {
		t.Fatal("duplicate request overwrote existing waiter")
	}
	cl.unsubscribeSessionReply("id", w)
	newer, err := cl.subscribeSessionReply("id", "session", protocol.TypeSessionResult)
	if err != nil {
		t.Fatal(err)
	}
	cl.unsubscribeSessionReply("id", w)
	cl.failWaiters()
	if _, err := cl.subscribeSessionReply("late", "session", protocol.TypeSessionResult); err == nil {
		t.Fatal("late subscriber missed link failure")
	}
	select {
	case msg := <-newer.ch:
		if msg.Code != protocol.SessionUnknown {
			t.Fatal("incorrect terminal")
		}
	default:
		t.Fatal("stale unsubscribe removed current waiter")
	}
	cl.unsubscribeSessionReply("id", newer)
}

// Transport loss and a valid terminal may race. Whichever is accepted first
// must be the sole result; neither path may block the shared connection reader.
func TestSessionReplies_shouldSettleOnce_whenResponseRacesDisconnect(t *testing.T) {
	for i := 0; i < 100; i++ {
		cl := &clientLink{}
		w, err := cl.subscribeSessionReply("id", "session", protocol.TypeSessionResumeResult)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cl.dispatch(protocol.Message{Type: protocol.TypeSessionResumeResult, RequestID: "id", SessionID: "session", Status: protocol.SessionActive})
		}()
		go func() { defer wg.Done(); cl.failWaiters() }()
		wg.Wait()
		select {
		case msg := <-w.ch:
			if msg.Status != protocol.SessionActive && msg.Status != protocol.SessionUnknownState {
				t.Fatal("invalid terminal")
			}
		default:
			t.Fatal("no terminal")
		}
		select {
		case <-w.ch:
			t.Fatal("multiple terminals")
		default:
		}
		cl.unsubscribeSessionReply("id", w)
	}
}

// Link failure resolves every independent pending operation once, retaining
// correlation IDs even when both requests refer to the same session.
func TestSessionReplies_shouldResolveEveryRequestUnknown_whenLinkFails(t *testing.T) {
	cl := &clientLink{}
	a := subscribeReplyForTest(t, cl, protocol.TypeSessionResumeResult, "a", "session")
	b := subscribeReplyForTest(t, cl, protocol.TypeSessionResumeResult, "b", "session")
	cl.failWaiters()
	for id, ch := range map[string]<-chan protocol.Message{"a": a, "b": b} {
		select {
		case got := <-ch:
			if got.RequestID != id || got.SessionID != "session" || got.Code != protocol.SessionUnknown {
				t.Errorf("unknown outcome lost correlation: %+v", got)
			}
		default:
			t.Errorf("request %s never settled", id)
		}
	}
}
