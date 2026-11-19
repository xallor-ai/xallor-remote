package noderun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type sessionRequestParams struct {
	DeviceID         string   `json:"device_id"`
	CodeID           string   `json:"code_id"`
	CodeSecret       string   `json:"code_secret"`
	TargetUID        string   `json:"target_uid"`
	Generation       uint64   `json:"generation"`
	Scope            []string `json:"scope"`
	SessionID        string   `json:"session_id,omitempty"`
	SessionExpiresAt *int64   `json:"session_expires_at,omitempty"`
}

func (d *Daemon) ipcSessionRequest(c *ipc.Conn, req ipc.Request) {
	var params sessionRequestParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "session.request 参数无效"))
		return
	}
	peer, ok := d.store.Peer(params.DeviceID)
	if !ok {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownDevice, ipc.Human(protocol.UnknownDevice)))
		return
	}
	sessionID := params.SessionID
	if sessionID == "" {
		var err error
		sessionID, err = protocol.RandomHex(16)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return
		}
	}
	requestID, err := protocol.RandomHex(16)
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	message := protocol.Message{Type: protocol.TypeSessionRequest, RequestID: requestID, SessionID: sessionID, TargetID: peer.DeviceID, TargetUID: params.TargetUID, CodeID: params.CodeID, CodeGeneration: params.Generation, SessionScope: append([]string(nil), params.Scope...), SessionExpiresAt: params.SessionExpiresAt, Grant: params.CodeSecret}
	if _, err := message.AccessCodeSessionRequest(); err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	link, client, err := d.ensureClient(peer)
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, clientDialCode(err), ipc.Human(clientDialCode(err))))
		return
	}
	waiter, err := client.subscribeSessionReply(requestID, sessionID, protocol.TypeSessionResult)
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.SessionUnknown, ipc.Human(protocol.SessionUnknown)))
		return
	}
	defer client.unsubscribeSessionReply(requestID, waiter)
	if err := link.send(message); err != nil {
		code := sessionSendFailureCode(err)
		_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, 90*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.SessionUnknown, "session 结果未确认。"))
			return
		case response := <-waiter.ch:
			if response.Type == protocol.TypeError {
				_ = c.WriteFrame(ipc.Fail(req.ID, response.Code, ipc.Human(response.Code)))
				return
			}
			if response.Type != protocol.TypeSessionResult {
				continue
			}
			switch response.Status {
			case protocol.SessionPending:
				continue
			case protocol.SessionActive:
				_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{
					"session_id": response.SessionID, "status": response.Status,
					"request_sha256": response.RequestSHA256, "generation": response.CodeGeneration,
					"scope": response.SessionScope, "session_expires_at": response.SessionExpiresAt,
					"resume_id": response.ResumeID, "resume_capability": response.ResumeCapability,
				}))
				return
			default:
				code := response.Code
				if code == "" {
					code = protocol.SessionUnknown
				}
				_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
				return
			}
		}
	}
}
