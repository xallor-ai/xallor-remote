package noderun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type sessionResumeParams struct {
	DeviceID         string `json:"device_id"`
	SessionID        string `json:"session_id"`
	ResumeID         string `json:"resume_id"`
	ResumeCapability string `json:"resume_capability"`
	TargetUID        string `json:"target_uid"`
}

func (d *Daemon) ipcSessionResume(c *ipc.Conn, req ipc.Request) {
	var params sessionResumeParams
	if json.Unmarshal(req.Params, &params) != nil || params.DeviceID == "" || params.SessionID == "" || params.ResumeID == "" || params.ResumeCapability == "" || params.TargetUID == "" {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "session.resume 参数无效"))
		return
	}
	peer, ok := d.store.Peer(params.DeviceID)
	if !ok {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownDevice, ipc.Human(protocol.UnknownDevice)))
		return
	}
	link, client, err := d.ensureClient(peer)
	if err != nil {
		code := clientDialCode(err)
		_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
		return
	}
	requestID, err := protocol.RandomHex(16)
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	message := protocol.Message{Type: protocol.TypeSessionResumeRequest, RequestID: requestID, SessionID: params.SessionID, ResumeID: params.ResumeID, ResumeCapability: params.ResumeCapability, TargetUID: params.TargetUID}
	if err := protocol.ValidateSessionResumeRequest(message); err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	waiter, err := client.subscribeSessionReply(requestID, params.SessionID, protocol.TypeSessionResumeResult)
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
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.SessionUnknown, "session resume 结果未确认。"))
			return
		case response := <-waiter.ch:
			if response.Type != protocol.TypeSessionResumeResult {
				continue
			}
			if response.Status == protocol.SessionActive {
				_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{
					"session_id": response.SessionID, "status": response.Status,
					"generation": response.CodeGeneration, "scope": response.SessionScope,
					"session_expires_at": response.SessionExpiresAt,
					"resume_id":          response.ResumeID, "resume_capability": response.ResumeCapability,
				}))
				return
			}
			code := response.Code
			if code == "" {
				code = protocol.SessionUnknown
			}
			_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
			return
		}
	}
}
