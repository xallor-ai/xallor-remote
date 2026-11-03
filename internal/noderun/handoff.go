package noderun

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/handoff"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) handleHandoffIPC(c *ipc.Conn, req ipc.Request) bool {
	if req.Method != protocol.OpHandoffSubmit && req.Method != protocol.OpHandoffGet {
		return false
	}
	go d.ipcOp(c, req, req.Method)
	return true
}
func (d *Daemon) handleHandoffInvoke(link *wsConn, msg protocol.Message) bool {
	if msg.Op != protocol.OpHandoffSubmit && msg.Op != protocol.OpHandoffGet {
		return false
	}
	var ticket handoff.Ticket
	if msg.Op == protocol.OpHandoffSubmit && msg.Scope != nil && d.handoffs != nil {
		ticket = d.handoffs.Capture(*msg.Scope, msg.Delegation)
	}
	go d.runHandoff(link, msg, ticket)
	return true
}
func (d *Daemon) runHandoff(link *wsConn, msg protocol.Message, ticket handoff.Ticket) {
	start := time.Now()
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	// Only the authenticated device link supplies this Relay-derived namespace.
	// Missing scope never falls back to an unscoped receipt directory.
	if msg.Scope == nil || !msg.Scope.Valid() {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.Unauthorized))
		return
	}
	var payload struct {
		Request handoff.Request `json:"request"`
		ID      string          `json:"request_id"`
	}
	if len(msg.Payload) > 64*1024 || json.Unmarshal(msg.Payload, &payload) != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	var receipt handoff.Receipt
	var err error
	if d.handoffs == nil {
		err = fmt.Errorf("交接服务不可用。")
	} else if msg.Op == protocol.OpHandoffSubmit {
		receipt, err = d.handoffs.Submit(ticket, payload.Request, workspace)
	} else {
		receipt, err = d.handoffs.Get(*msg.Scope, payload.ID)
	}
	// Application rejection is a structured response; the surrounding exit only
	// confirms this short submit/query, never completion of the Agent job.
	result := map[string]any{"receipt": receipt}
	if err != nil {
		result = map[string]any{"error": err.Error()}
	}
	data, _ := json.Marshal(result)
	if sendStdoutFrames(link, msg.ExecID, data) != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	d.sendOKExit(link, msg.ExecID, start, false)
}
