package noderun

import (
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"sort"
)

type approvalView struct {
	ID      string `json:"id"`
	ExecID  string `json:"exec_id"`
	Preview string `json:"preview"`
	Created string `json:"created"`
}

func (h *approvalHub) list() []approvalView {
	h.mu.Lock()
	defer h.mu.Unlock()
	items := make([]approvalView, 0, len(h.pending))
	for id, p := range h.pending {
		items = append(items, approvalView{ID: id, ExecID: p.execID, Preview: p.preview, Created: p.created})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Created < items[j].Created || items[i].Created == items[j].Created && items[i].ID < items[j].ID
	})
	return items
}

func (d *Daemon) handleHeadState(c *ipc.Conn, req ipc.Request) bool {
	switch req.Method {
	case "approval.list":
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"pending": d.approvals.list()}))
	case "audit.list":
		if d.activity == nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "审计记录不可用。"))
			return true
		}
		rows, err := d.activity.Entries()
		if err != nil {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, err.Error()))
			return true
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"entries": rows}))
	default:
		return false
	}
	return true
}
