package noderun

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/shell"
)

func (d *Daemon) handleLocal(c *ipc.Conn, req ipc.Request) bool {
	switch req.Method {
	case "peer.rename":
		var p struct {
			DeviceID string  `json:"device_id"`
			Name     *string `json:"name"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.DeviceID == "" || p.Name == nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要设备 ID 与备注。"))
			return true
		}
		if err := d.store.RenamePeer(p.DeviceID, *p.Name); err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return true
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]bool{"ok": true}))
	case "revoke":
		ctx, cancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
		defer cancel()
		release, err := d.settings.acquire(ctx)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.QuotaExceeded, "设备设置正在更新，请稍后再试。"))
			return true
		}
		defer release()
		if err := d.store.ClearGrant(); err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "无法清除本机授权码。"))
			return true
		}
		d.pushRevoke(ctx)
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
	case "reset":
		ctx, cancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
		defer cancel()
		release, err := d.settings.acquire(ctx)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.QuotaExceeded, "设备设置正在更新，请稍后再试。"))
			return true
		}
		defer release()
		var p struct {
			Confirm bool `json:"confirm"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if !p.Confirm {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 confirm=true"))
			return true
		}
		d.pushRevoke(ctx)
		if err := d.store.Wipe(); err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "本机重置未完成，请检查数据目录后重启服务。"))
			d.Stop()
			return true
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
		d.Stop()
	case "stop":
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
		d.Stop()
	case "peer.list":
		_, _, _, _, _, _, peers := d.store.Snapshot()
		ids := make([]string, 0, len(peers))
		for _, p := range peers {
			ids = append(ids, p.DeviceID)
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"peers": ids}))
	case "peer.remove":
		var p struct {
			DeviceID string `json:"device_id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.DeviceID == "" {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 device_id"))
			return true
		}
		if err := d.store.RemovePeer(p.DeviceID); err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return true
		}
		d.dropClient(p.DeviceID, nil)
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
	case "config.get":
		cfg := d.store.ConfigSnapshot()
		if cfg.Shell == "" {
			cfg.Shell = "auto"
		}
		_ = c.WriteFrame(ipc.OK(req.ID, cfg))
	case "config.set":
		ctx, cancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
		defer cancel()
		release, err := d.settings.acquire(ctx)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.QuotaExceeded, "设备设置正在更新，请稍后再试。"))
			return true
		}
		defer release()
		var p struct {
			RelayURL  string `json:"relay_url"`
			Workspace string `json:"workspace"`
			Shell     string `json:"shell"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "配置格式无效。"))
			return true
		}
		oldConfig := d.store.ConfigSnapshot()
		if err := d.store.SetConfig(p.RelayURL, p.Workspace, p.Shell); err != nil {
			code := protocol.AgentError
			if errors.Is(err, shell.ErrUnsupported) {
				code = protocol.UnsupportedShell
			}
			_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
			return true
		}
		newConfig := d.store.ConfigSnapshot()
		if oldConfig.RelayURL != newConfig.RelayURL {
			d.mu.Lock()
			device := d.device
			clients := make([]*clientLink, 0, len(d.clients))
			for key, client := range d.clients {
				delete(d.clients, key)
				clients = append(clients, client)
			}
			d.mu.Unlock()
			if device != nil {
				_ = device.conn.CloseNow()
			}
			for _, client := range clients {
				d.cancelRemotesForLink(client.ws)
				client.failWaiters()
				client.closeConn()
			}
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
	default:
		return false
	}
	return true
}

func (d *Daemon) pushRevoke(ctx context.Context) {
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil {
		return
	}
	_ = link.sendContext(ctx, protocol.Message{Type: protocol.TypeRevoke})
}
