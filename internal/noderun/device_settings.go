package noderun

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Settings mutations and initial connection snapshots share one bounded gate.
// Never hold Daemon.mu during network I/O or while waiting for this gate.
type settingsGate struct {
	once          sync.Once
	slots, writer chan struct{}
}

func (g *settingsGate) acquire(ctx context.Context) (func(), error) {
	g.once.Do(func() { g.slots = make(chan struct{}, 16); g.writer = make(chan struct{}, 1) })
	select {
	case g.slots <- struct{}{}:
	default:
		return nil, errStr(protocol.QuotaExceeded)
	}
	select {
	case g.writer <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.writer
			<-g.slots
			return nil, err
		}
		return func() { <-g.writer; <-g.slots }, nil
	case <-ctx.Done():
		<-g.slots
		return nil, ctx.Err()
	}
}

func settingsMessage(grant string, inbound bool) protocol.Message {
	hash := ""
	if grant != "" {
		hash = protocol.SHA256Hex(grant)
	}
	return protocol.Message{Type: protocol.TypeDeviceSettings, GrantHash: hash, Inbound: protocol.Bool(inbound)}
}

func (d *Daemon) handleDeviceSettings(c *ipc.Conn, req ipc.Request) bool {
	switch req.Method {
	case "grant.issue", "grant.rotate", "inbound.set":
	default:
		return false
	}
	var enabled *bool
	if req.Method == "inbound.set" {
		var p struct {
			Enabled *bool `json:"enabled"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Enabled == nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 enabled"))
			return true
		}
		enabled = p.Enabled
	}
	_ = c.WriteFrame(d.updateDeviceSettings(req.ID, enabled))
	return true
}

func (d *Daemon) updateDeviceSettings(id string, enabled *bool) ipc.Frame {
	ctx, cancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
	defer cancel()
	release, err := d.settings.acquire(ctx)
	if err != nil {
		return ipc.Fail(id, protocol.QuotaExceeded, "设备设置正在更新，请稍后再试。")
	}
	defer release()
	var grant string
	if enabled == nil {
		grant, err = d.store.IssueGrant()
	} else {
		grant, err = d.store.SetInbound(*enabled)
	}
	if err != nil {
		return ipc.Fail(id, protocol.AgentError, "无法保存设备设置。")
	}
	on := enabled == nil || *enabled
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	unknown := func() ipc.Frame {
		return ipc.Fail(id, protocol.RelayError, "设置已保存在本机，尚未确认在中转生效。连接恢复后会同步；授权码可用 grant show 查看。")
	}
	if link == nil {
		return unknown()
	}
	if link.relay != d.store.ConfigSnapshot().RelayURL {
		_ = link.conn.CloseNow()
		return unknown()
	}
	key, reply, remove, err := d.settingsReplies.register(link)
	if err != nil {
		return unknown()
	}
	defer remove()
	msg := settingsMessage(grant, on)
	msg.RequestID = key
	if err := link.sendContext(ctx, msg); err == nil {
		select {
		case result := <-reply:
			if result.Code == "" {
				out := map[string]any{"inbound": on}
				if on {
					out["grant"] = grant
				}
				return ipc.OK(id, out)
			}
		case <-ctx.Done():
		case <-link.ctx.Done():
		}
	}
	// A fresh connection reconciles the saved snapshot; it never repeats IssueGrant.
	_ = link.conn.CloseNow()
	return unknown()
}
