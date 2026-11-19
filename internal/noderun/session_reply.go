package noderun

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Session operations return one terminal, not a stream. clientLink.mu protects
// the registry and each waiter's finished bit, including transport failure.
type sessionReplyWaiter struct {
	sessionID string
	kind      string
	ch        chan protocol.Message
	finished  bool
}

func (cl *clientLink) subscribeSessionReply(requestID, sessionID, kind string) (*sessionReplyWaiter, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if requestID == "" || sessionID == "" || (kind != protocol.TypeSessionResult && kind != protocol.TypeSessionResumeResult) {
		return nil, errors.New("invalid session reply correlation")
	}
	if cl.sessionRepliesClosed {
		return nil, errors.New("session reply transport closed")
	}
	if cl.sessionReplies[requestID] != nil {
		return nil, errors.New("session request already registered")
	}
	if cl.sessionReplies == nil {
		cl.sessionReplies = make(map[string]*sessionReplyWaiter)
	}
	w := &sessionReplyWaiter{sessionID: sessionID, kind: kind, ch: make(chan protocol.Message, 1)}
	cl.sessionReplies[requestID] = w
	return w, nil
}

func (cl *clientLink) unsubscribeSessionReply(requestID string, expected *sessionReplyWaiter) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.sessionReplies[requestID] == expected {
		delete(cl.sessionReplies, requestID)
	}
}

func (cl *clientLink) dispatchSessionReply(msg protocol.Message) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	w := cl.sessionReplies[msg.RequestID]
	if w == nil || w.kind != msg.Type || w.sessionID != msg.SessionID {
		return
	}
	switch msg.Status {
	case protocol.SessionActive, protocol.SessionRejectedState, protocol.SessionExpiredState, protocol.SessionUnknownState:
		w.finish(msg)
	}
}

// Called under clientLink.mu. Progress frames are never queued, so the sole
// terminal always fits without blocking the shared connection reader.
func (w *sessionReplyWaiter) finish(msg protocol.Message) {
	if w.finished {
		return
	}
	w.finished = true
	w.ch <- msg
}

// Called under clientLink.mu. Mark the link closed before resolving registered
// operations, preventing a late subscriber from missing transport loss.
func (cl *clientLink) failSessionReplies() {
	cl.sessionRepliesClosed = true
	for id, w := range cl.sessionReplies {
		w.finish(protocol.Message{Type: w.kind, RequestID: id, SessionID: w.sessionID,
			Status: protocol.SessionUnknownState, Code: protocol.SessionUnknown})
	}
}

// send does not establish peer receipt or transaction outcome. Only its local
// message-size gate proves that no frame was admitted. Never infer safe replay
// from a transport error after attempting a session mutation.
func sessionSendFailureCode(err error) string {
	if errors.Is(err, errMsgTooLarge) {
		return protocol.TooLarge
	}
	return protocol.SessionUnknown
}
