package noderun

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type accessWaiter struct {
	link   *wsConn
	result chan protocol.Message
}
type accessRequests struct {
	mu      sync.Mutex
	pending map[string]accessWaiter
}

func (r *accessRequests) register(link *wsConn) (string, <-chan protocol.Message, func(), error) {
	id, err := protocol.RandomHex(16)
	if err != nil {
		return "", nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= 16 {
		return "", nil, nil, errStr(protocol.QuotaExceeded)
	}
	if r.pending == nil {
		r.pending = map[string]accessWaiter{}
	}
	result := make(chan protocol.Message, 1)
	r.pending[id] = accessWaiter{link: link, result: result}
	return id, result, func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }, nil
}

func (r *accessRequests) deliver(link *wsConn, msg protocol.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	waiter, ok := r.pending[msg.RequestID]
	if !ok || waiter.link != link {
		return
	}
	select {
	case waiter.result <- msg:
	default:
	}
}

func (d *Daemon) handleDeviceAccess(c *ipc.Conn, request ipc.Request) bool {
	if request.Method != "device.access" {
		return false
	}
	var parameters access.Request
	if len(request.Params) > 16*1024 || json.Unmarshal(request.Params, &parameters) != nil || parameters.Validate() != nil {
		_ = c.WriteFrame(ipc.Fail(request.ID, protocol.AgentError, "设备归属或分享参数无效。"))
		return true
	}
	go func() { _ = c.WriteFrame(d.manageDeviceAccess(request.ID, parameters)) }()
	return true
}

func (d *Daemon) manageDeviceAccess(id string, parameters access.Request) ipc.Frame {
	relay, token, err := d.store.Credentials()
	if err != nil || token == "" {
		return ipc.Fail(id, protocol.Unauthorized, "请先导入当前空间的身份凭据。")
	}
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil || link.relay != relay {
		return ipc.Fail(id, protocol.RelayError, "本机尚未连接当前中转。")
	}
	ctx, cancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
	defer cancel()
	key, reply, remove, err := d.accessRequests.register(link)
	if err != nil {
		return ipc.Fail(id, protocol.QuotaExceeded, "设备管理请求过多，请稍后再试。")
	}
	defer remove()
	payload, _ := json.Marshal(parameters)
	unknown := func() ipc.Frame {
		return ipc.Fail(id, protocol.RelayError, "操作结果未确认，请查看设备归属和分享状态后再决定是否重试。")
	}
	if err := link.sendContext(ctx, protocol.Message{Type: protocol.TypeAccessRequest, RequestID: key, AccountToken: token, Payload: payload}); err != nil {
		return unknown()
	}
	select {
	case result := <-reply:
		if result.Code == protocol.RelayError {
			return unknown()
		}
		if result.Code != "" {
			return ipc.Fail(id, result.Code, ipc.Human(result.Code))
		}
		var state access.State
		if json.Unmarshal(result.Payload, &state) != nil || state.DeviceUID == "" {
			return unknown()
		}
		return ipc.OK(id, state)
	case <-ctx.Done():
		return unknown()
	case <-link.ctx.Done():
		return unknown()
	}
}
