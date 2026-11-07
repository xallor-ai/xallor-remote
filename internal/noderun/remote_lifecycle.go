package noderun

import (
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Returns a locked task. Caller sends invoke then releases r.mu, so an IPC
// disconnect cannot send cancel before the invoke it is meant to cancel.
func (d *Daemon) trackRemote(execID string, link *wsConn, c *ipc.Conn) *remoteExec {
	r := &remoteExec{link: link, ipc: c}
	r.mu.Lock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if c.Closed() {
		r.mu.Unlock()
		return nil
	}
	d.remote[execID] = r
	return r
}

func (d *Daemon) untrackRemote(execID string) {
	d.mu.Lock()
	delete(d.remote, execID)
	d.mu.Unlock()
}

func cancelRemote(r *remoteExec, execID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sent {
		_ = r.link.send(protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpCancel})
	}
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
	cancelRemote(r, p.ExecID)
	_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
}

func (d *Daemon) cancelAllForConn(c *ipc.Conn) {
	d.mu.Lock()
	pending := map[string]*remoteExec{}
	for id, r := range d.remote {
		if r.ipc == c {
			pending[id] = r
		}
	}
	d.mu.Unlock()
	for id, r := range pending {
		cancelRemote(r, id)
	}
}

func (d *Daemon) cancelRemotesForLink(link *wsConn) {
	d.mu.Lock()
	pending := map[string]*remoteExec{}
	for id, r := range d.remote {
		if r.link == link {
			pending[id] = r
		}
	}
	d.mu.Unlock()
	for id, r := range pending {
		cancelRemote(r, id)
	}
}
