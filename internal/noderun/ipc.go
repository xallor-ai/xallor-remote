package noderun

import (
	"encoding/json"

	"github.com/xallor-ai/xallor-remote/internal/activity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) serveIPC(c *ipc.Conn) {
	defer func() { _ = c.Close(); d.cancelAllForConn(c) }()
	defer d.approvals.unsubscribe(c)
	for {
		req, err := c.ReadRequest()
		if err != nil {
			return
		}
		if d.handleAccount(c, req) || d.handleObservation(c, req) || d.handleHeadState(c, req) || d.handleHandoffIPC(c, req) || d.handleLocal(c, req) {
			continue
		}
		switch req.Method {
		case "grant.issue":
			g, err := d.store.IssueGrant()
			if err != nil {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
				continue
			}
			d.pushGrant(g)
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"grant": g}))
		case "grant.show":
			_, _, g, _, _, _, _ := d.store.Snapshot()
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"grant": g}))
		case "peer.add":
			var p struct {
				DeviceID string `json:"device_id"`
				Grant    string `json:"grant"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.DeviceID == "" || p.Grant == "" {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 device_id 与 grant"))
				continue
			}
			if err := d.store.AddPeer(p.DeviceID, p.Grant); err != nil {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
				continue
			}
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
		case "approval.subscribe":
			d.approvals.subscribe(c)
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
		case "approval.respond":
			var p struct {
				Allow      *bool  `json:"allow"`
				ApprovalID string `json:"approval_id"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Allow == nil || p.ApprovalID == "" {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 approval_id 与 allow"))
				continue
			}
			if !d.approvals.respond(p.ApprovalID, *p.Allow) {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownExec, ipc.Human(protocol.UnknownExec)))
				continue
			}
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
		case "exec":
			go d.ipcExec(c, req)
		case "exec.cancel":
			d.ipcCancel(c, req)
		case "read":
			go d.ipcOp(c, req, protocol.OpRead)
		case "write":
			go d.ipcOp(c, req, protocol.OpWrite)
		case "processes":
			go d.ipcOp(c, req, protocol.OpProcesses)
		case "info":
			go d.ipcOp(c, req, protocol.OpInfo)
		default:
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "未知方法"))
		}
	}
}

func (d *Daemon) ipcExec(c *ipc.Conn, req ipc.Request) {
	var p struct {
		DeviceID  string `json:"device_id"`
		Command   string `json:"command"`
		Cwd       string `json:"cwd"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Command == "" {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 command"))
		return
	}
	peer, ok := d.store.Peer(p.DeviceID)
	if !ok {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownDevice, ipc.Human(protocol.UnknownDevice)))
		return
	}
	link, cl, err := d.ensureClient(peer)
	if err != nil {
		code := clientDialCode(err)
		_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
		return
	}
	execID, err := protocol.NewExecID()
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	payload, _ := json.Marshal(protocol.ExecPayload{Command: p.Command, Cwd: p.Cwd, TimeoutMS: p.TimeoutMS})
	d.forwardInvoke(c, req.ID, peer.DeviceID, link, cl, protocol.OpExec, execID, payload, true)
}

func (d *Daemon) ipcOp(c *ipc.Conn, req ipc.Request, op string) {
	var p struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.Unmarshal(req.Params, &p)
	peer, ok := d.store.Peer(p.DeviceID)
	if !ok {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownDevice, ipc.Human(protocol.UnknownDevice)))
		return
	}
	link, cl, err := d.ensureClient(peer)
	if err != nil {
		code := clientDialCode(err)
		_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
		return
	}
	execID, err := protocol.NewExecID()
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	d.forwardInvoke(c, req.ID, peer.DeviceID, link, cl, op, execID, req.Params, false)
}

func (d *Daemon) forwardInvoke(c *ipc.Conn, reqID, deviceID string, link *wsConn, cl *clientLink, op, execID string, payload json.RawMessage, stream bool) {
	r := d.trackRemote(execID, link, c)
	if r == nil {
		return
	}
	d.recordActivity(activity.Entry{ExecID: execID, Direction: "outbound", DeviceID: deviceID, Op: op, Stage: "requested"})
	defer d.untrackRemote(execID)
	w := cl.subscribe(execID)
	defer cl.unsubscribe(execID)
	err := link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: op, Payload: payload})
	r.sent = err == nil
	r.mu.Unlock()
	if err != nil {
		if err == errMsgTooLarge {
			d.recordTerminal("outbound", deviceID, protocol.Nack(execID, protocol.TooLarge))
			_ = c.WriteFrame(ipc.Fail(reqID, protocol.TooLarge, ipc.Human(protocol.TooLarge)))
			return
		}
		d.recordTerminal("outbound", deviceID, protocol.Err(execID, protocol.RelayError))
		d.dropClient(deviceID, cl)
		_ = c.WriteFrame(ipc.Fail(reqID, protocol.RelayError, ipc.Human(protocol.RelayError)))
		return
	}
	if stream {
		if c.WriteFrame(ipc.Frame{ID: reqID, Event: "stdout", ExecID: execID}) != nil {
			d.recordTerminal("outbound", deviceID, protocol.Err(execID, protocol.RelayError))
			cancelRemote(r, execID)
			return
		}
	}
	d.consumeClient(c, reqID, execID, deviceID, link, w, stream)
}

func (d *Daemon) consumeClient(ipcConn *ipc.Conn, reqID, execID, deviceID string, link *wsConn, w *execWaiter, stream bool) {
	var collected string
	for {
		msg, ok := w.next()
		if !ok {
			d.recordTerminal("outbound", deviceID, protocol.Err(execID, protocol.RelayError))
			_ = ipcConn.WriteFrame(ipc.Fail(reqID, protocol.RelayError, ipc.Human(protocol.RelayError)))
			return
		}
		d.recordTerminal("outbound", deviceID, msg)
		switch msg.Type {
		case protocol.TypeStdout:
			if stream {
				if err := ipcConn.WriteFrame(ipc.Frame{ID: reqID, Event: "stdout", Data: msg.Data, ExecID: execID}); err != nil {
					_ = link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
					return
				}
			} else {
				collected += msg.Data
			}
		case protocol.TypeStderr:
			if stream {
				if err := ipcConn.WriteFrame(ipc.Event(reqID, "stderr", msg.Data)); err != nil {
					_ = link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
					return
				}
			}
		case protocol.TypeExit:
			dur := int64(0)
			if msg.DurationMS != nil {
				dur = *msg.DurationMS
			}
			trunc := false
			if msg.Truncated != nil {
				trunc = *msg.Truncated
			}
			res := map[string]any{
				"exit_code": msg.ExitCode, "duration_ms": dur, "truncated": trunc,
				"status": msg.Status, "exec_id": execID,
			}
			if !stream {
				res["content"] = collected
			}
			_ = ipcConn.WriteFrame(ipc.OK(reqID, res))
			return
		case protocol.TypeInvokeNack, protocol.TypeError:
			_ = ipcConn.WriteFrame(ipc.Fail(reqID, msg.Code, ipc.Human(msg.Code)))
			return
		}
	}
}
