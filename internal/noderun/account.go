package noderun

import (
	"encoding/json"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) handleAccount(c *ipc.Conn, req ipc.Request) bool {
	switch req.Method {
	case "account.status":
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]bool{"configured": d.store.AccountConfigured()}))
	case "account.set":
		var p struct {
			Token *string `json:"token"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Token == nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要身份凭据"))
			return true
		}
		err := d.setAccount(*p.Token)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
		} else {
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]bool{"configured": *p.Token != ""}))
		}
	default:
		return false
	}
	return true
}

func (d *Daemon) setAccount(token string) error {
	d.mu.Lock()
	err := d.store.SetAccount(token)
	var old []*clientLink
	if err == nil {
		d.accountGeneration++
		for key, cl := range d.clients {
			old = append(old, cl)
			delete(d.clients, key)
		}
	}
	d.mu.Unlock()
	for _, cl := range old {
		cl.failWaiters()
		cl.closeConn()
	}
	return err
}
