package noderun

import (
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
	case "grant.rotate":
		g, err := d.store.RotateGrant()
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return true
		}
		d.pushGrant(g)
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"grant": g}))
	case "inbound.set":
		var p struct {
			Enabled *bool `json:"enabled"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.Enabled == nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 enabled"))
			return true
		}
		g, err := d.store.SetInbound(*p.Enabled)
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return true
		}
		if *p.Enabled {
			d.pushGrant(g)
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"inbound": true, "grant": g}))
		} else {
			d.pushInbound(false)
			_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"inbound": false}))
		}
	case "revoke":
		d.pushRevoke()
		_ = d.store.ClearGrant()
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
	case "reset":
		var p struct {
			Confirm bool `json:"confirm"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if !p.Confirm {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要 confirm=true"))
			return true
		}
		d.pushRevoke()
		_ = d.store.Wipe()
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
		var p struct {
			RelayURL  string `json:"relay_url"`
			Workspace string `json:"workspace"`
			Shell     string `json:"shell"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "配置格式无效。"))
			return true
		}
		if err := d.store.SetConfig(p.RelayURL, p.Workspace, p.Shell); err != nil {
			code := protocol.AgentError
			if errors.Is(err, shell.ErrUnsupported) {
				code = protocol.UnsupportedShell
			}
			_ = c.WriteFrame(ipc.Fail(req.ID, code, ipc.Human(code)))
			return true
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]string{"ok": "1"}))
	default:
		return false
	}
	return true
}

func (d *Daemon) pushInbound(on bool) {
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil {
		return
	}
	_ = link.send(protocol.Message{Type: protocol.TypeInboundSet, Inbound: protocol.Bool(on)})
}

func (d *Daemon) pushRevoke() {
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil {
		return
	}
	_ = link.send(protocol.Message{Type: protocol.TypeRevoke})
}
