package noderun

import (
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/diagnostics"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Public status is a whitelist, never a serialization of the credential store.
func (d *Daemon) status() map[string]any {
	id, _, grant, ws, relay, inbound, peers := d.store.Snapshot()
	public := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		public = append(public, map[string]any{"device_id": peer.DeviceID, "name": peer.Name, "has_grant": peer.Grant != ""})
	}
	return map[string]any{"device_id": id, "workspace": ws, "relay": diagnostics.Endpoint(relay),
		"inbound": inbound, "has_grant": grant != "", "online": d.deviceOnline(),
		"version": buildinfo.Version(), "build": buildinfo.Running(), "peers": public}
}

func (d *Daemon) handleObservation(c *ipc.Conn, req ipc.Request) bool {
	switch req.Method {
	case "status":
		_ = c.WriteFrame(ipc.OK(req.ID, d.status()))
	case "doctor":
		var p struct {
			DeviceID string `json:"device_id"`
		}
		if json.Unmarshal(req.Params, &p) != nil || (p.DeviceID != "" && !protocol.IsNumericDeviceID(p.DeviceID)) {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要有效的 9 位设备 ID。"))
			return true
		}
		go func() { _ = c.WriteFrame(ipc.OK(req.ID, d.diagnose(p.DeviceID))) }()
	default:
		return false
	}
	return true
}

func (d *Daemon) diagnose(device string) diagnostics.Report {
	r := diagnostics.New(device)
	r.Set("ipc", "ok", "", "")
	if d.deviceOnline() {
		r.Set("runtime_relay", "ok", "", "")
		r.OK = device == ""
	} else {
		r.Set("runtime_relay", "fail", "disconnected", "检查本机网络和中转地址。")
	}
	if device == "" {
		return r
	}
	peer, ok := d.store.Peer(device)
	if !ok || peer.Grant == "" {
		r.Set("peer", "fail", "not_paired", "请先添加对方设备 ID 和授权码。")
		return r
	}
	r.Set("peer", "ok", "", "")
	relay, token, err := d.store.Credentials()
	if err != nil {
		r.Set("peer", "fail", protocol.Unauthorized, err.Error())
		return r
	}
	diagnostics.Probe(d.ctx, relay, device, peer.Grant, token, &r)
	for _, check := range r.Checks {
		if check.State == "fail" || check.State == "unknown" {
			r.OK = false
		}
	}
	return r
}
