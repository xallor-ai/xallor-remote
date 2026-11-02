package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Frame struct {
	ID      string          `json:"id,omitempty"`
	Event   string          `json:"event,omitempty"`
	Data    string          `json:"data,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	ExecID  string          `json:"exec_id,omitempty"`
}

type Conn struct {
	c      net.Conn
	mu     sync.Mutex
	rd     *bufio.Reader
	closed atomic.Bool
}

func NewConn(c net.Conn) *Conn {
	return &Conn{c: c, rd: bufio.NewReader(c)}
}

func (c *Conn) ReadRequest() (Request, error) {
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return Request{}, err
	}
	var r Request
	if err := json.Unmarshal(line, &r); err != nil {
		return Request{}, err
	}
	return r, nil
}

func (c *Conn) ReadFrame() (Frame, error) {
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func (c *Conn) WriteFrame(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.c.Write(append(b, '\n'))
	return err
}

func (c *Conn) Close() error {
	c.closed.Store(true)
	return c.c.Close()
}

func (c *Conn) Closed() bool { return c.closed.Load() }

func OK(id string, result any) Frame {
	raw, _ := json.Marshal(result)
	t := true
	return Frame{ID: id, OK: &t, Result: raw}
}

func Fail(id, code, message string) Frame {
	f := false
	return Frame{ID: id, OK: &f, Code: code, Message: message}
}

func Event(id, ev, data string) Frame {
	return Frame{ID: id, Event: ev, Data: data}
}

func EventParams(id, ev string, params any) Frame {
	raw, _ := json.Marshal(params)
	return Frame{ID: id, Event: ev, Params: raw}
}

func Call(c *Conn, id, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req := Request{ID: id, Method: method, Params: raw}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.c.Write(append(b, '\n'))
	return err
}

func Human(code string) string {
	switch code {
	case "unauthorized":
		return "当前身份或授权不可用，请检查身份凭据和设备访问权限。"
	case "unknown_device":
		return "找不到这台设备。"
	case "device_offline":
		return "目标不在线。"
	case "inbound_disabled":
		return "对方尚未开放入站。"
	case "unknown_exec":
		return "没有这条任务。"
	case "relay_error":
		return "中转已断开，请稍后重试。"
	case "exec_timeout":
		return "执行超时。"
	case "cancelled":
		return "任务已取消。"
	case "policy_deny":
		return "策略拒绝。"
	case "approval_timeout":
		return "等待确认超时。请在本机批准后重试。"
	case "too_large":
		return "内容超过上限。"
	case "workspace_missing":
		return "workspace 目录不可用。"
	case "quota_exceeded":
		return "已达到资源上限，请稍后再试。"
	default:
		if code == "" {
			return "失败。"
		}
		return fmt.Sprintf("失败（%s）。", code)
	}
}
