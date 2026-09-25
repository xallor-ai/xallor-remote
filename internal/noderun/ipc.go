package noderun

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) serveIPC(c *ipc.Conn) {
	defer c.Close()
	defer d.approvals.unsubscribe(c)
	defer d.cancelAllForConn(c)
	for {
		req, err := c.ReadRequest()
		if err != nil {
			return
		}
		if d.handleLocal(c, req) {
			continue
		}
		switch req.Method {
		case "status":
			id, _, grant, ws, relay, inbound, peers := d.store.Snapshot()
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{
				"device_id": id, "workspace": ws, "relay": relay,
				"inbound": inbound, "has_grant": grant != "",
				"online": d.deviceOnline(), "version": protocol.Version,
				"peers": peers,
			}))
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
				Allow *bool `json:"allow"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Allow == nil {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 allow"))
				continue
			}
			if !d.approvals.respond(req.ID, *p.Allow) {
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
	cl.mu.Lock()
	defer cl.mu.Unlock()
	execID, err := protocol.NewExecID()
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	payload, _ := json.Marshal(protocol.ExecPayload{Command: p.Command, Cwd: p.Cwd, TimeoutMS: p.TimeoutMS})
	d.forwardInvoke(c, req.ID, peer.DeviceID, link, protocol.OpExec, execID, payload, true)
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
	cl.mu.Lock()
	defer cl.mu.Unlock()
	execID, err := protocol.NewExecID()
	if err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		return
	}
	d.forwardInvoke(c, req.ID, peer.DeviceID, link, op, execID, req.Params, false)
}

func (d *Daemon) forwardInvoke(c *ipc.Conn, reqID, deviceID string, link *wsConn, op, execID string, payload json.RawMessage, stream bool) {
	d.trackRemote(execID, link, c)
	defer d.untrackRemote(execID)
	if err := link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: op, Payload: payload}); err != nil {
		d.dropClient(deviceID)
		_ = c.WriteFrame(ipc.Fail(reqID, protocol.RelayError, ipc.Human(protocol.RelayError)))
		return
	}
	if stream {
		_ = c.WriteFrame(ipc.Frame{ID: reqID, Event: "stdout", ExecID: execID})
	}
	d.readClientLoop(c, reqID, execID, deviceID, link, stream)
}

func (d *Daemon) ensureClient(peer identity.Peer) (*wsConn, *clientLink, error) {
	d.mu.Lock()
	if cl, ok := d.clients[peer.DeviceID]; ok && cl.ws != nil {
		d.mu.Unlock()
		return cl.ws, cl, nil
	}
	d.mu.Unlock()

	_, _, _, _, relay, _, _ := d.store.Snapshot()
	ctx, cancel := context.WithCancel(d.ctx)
	conn, _, err := websocket.Dial(ctx, relay, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	link := &wsConn{ctx: ctx, conn: conn}
	hello := protocol.Message{Type: protocol.TypeHelloClient, TargetID: peer.DeviceID, Grant: peer.Grant}
	if err := link.send(hello); err != nil {
		cancel()
		return nil, nil, err
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		cancel()
		return nil, nil, err
	}
	if ack.Type != protocol.TypeHelloOK {
		cancel()
		return nil, nil, fmt.Errorf("%s", ack.Code)
	}
	cl := &clientLink{target: peer.DeviceID, ws: link, cancel: cancel}
	d.mu.Lock()
	d.clients[peer.DeviceID] = cl
	d.mu.Unlock()
	go func() {
		t := time.NewTicker(protocol.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-d.ctx.Done():
				d.dropClient(peer.DeviceID)
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := link.send(protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
					d.dropClient(peer.DeviceID)
					return
				}
			}
		}
	}()
	return link, cl, nil
}

func (d *Daemon) readClientLoop(ipcConn *ipc.Conn, reqID, execID, deviceID string, link *wsConn, stream bool) {
	var collected string
	for {
		var msg protocol.Message
		if err := wsjson.Read(link.ctx, link.conn, &msg); err != nil {
			d.dropClient(deviceID)
			_ = ipcConn.WriteFrame(ipc.Fail(reqID, protocol.RelayError, ipc.Human(protocol.RelayError)))
			return
		}
		if msg.ExecID != "" && msg.ExecID != execID {
			continue
		}
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
			code := 0
			if msg.ExitCode != nil {
				code = *msg.ExitCode
			}
			dur := int64(0)
			if msg.DurationMS != nil {
				dur = *msg.DurationMS
			}
			trunc := false
			if msg.Truncated != nil {
				trunc = *msg.Truncated
			}
			res := map[string]any{
				"exit_code": code, "duration_ms": dur, "truncated": trunc,
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

func (d *Daemon) trackRemote(execID string, link *wsConn, c *ipc.Conn) {
	d.mu.Lock()
	d.remote[execID] = &remoteExec{link: link, ipc: c}
	d.mu.Unlock()
}

func (d *Daemon) untrackRemote(execID string) {
	d.mu.Lock()
	delete(d.remote, execID)
	d.mu.Unlock()
}

func (d *Daemon) ipcCancel(c *ipc.Conn, req ipc.Request) {
	var p struct {
		ExecID string `json:"exec_id"`
	}
	_ = json.Unmarshal(req.Params, &p)
	d.mu.Lock()
	r := d.remote[p.ExecID]
	d.mu.Unlock()
	if r == nil || r.ipc != c {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.UnknownExec, ipc.Human(protocol.UnknownExec)))
		return
	}
	_ = r.link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: p.ExecID, Op: protocol.OpCancel})
	_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
}

func (d *Daemon) cancelAllForConn(c *ipc.Conn) {
	d.mu.Lock()
	var pending []*remoteExec
	var ids []string
	for id, r := range d.remote {
		if r.ipc == c {
			pending = append(pending, r)
			ids = append(ids, id)
		}
	}
	d.mu.Unlock()
	for i, r := range pending {
		_ = r.link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: ids[i], Op: protocol.OpCancel})
	}
}
