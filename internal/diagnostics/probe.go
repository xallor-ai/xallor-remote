package diagnostics

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Probe opens a disposable client connection. It never registers a device,
// changes credentials/inbound, executes a shell command or reuses live tasks.
func Probe(ctx context.Context, relay, device, grant, accountToken string, r *Report) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, relay, nil)
	if err != nil {
		r.Set("relay_transport", "fail", transportCode(err), "检查中转地址、网络和证书。")
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	r.Set("relay_transport", "ok", "", "")
	r.Set("authorization", "unknown", "", "")
	err = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloClient, TargetID: device, Grant: grant, AccountToken: accountToken})
	var msg protocol.Message
	if err == nil {
		err = wsjson.Read(ctx, c, &msg)
	}
	if err != nil {
		r.Set("authorization", "unknown", transportCode(err), "中转未完成应答，请检查网络后重新诊断。")
		return
	}
	if msg.Type != protocol.TypeHelloOK {
		setFailure(r, msg.Code)
		return
	}
	if !msg.ConfirmsClientAuth(accountToken != "") {
		r.Set("authorization", "unknown", "invalid_response", "中转未确认连接认证，请检查版本。")
		return
	}
	r.Set("authorization", "ok", "", "")
	r.Set("target_online", "unknown", "", "")
	execID, err := protocol.NewExecID()
	if err != nil {
		r.Set("target_runtime", "unknown", protocol.AgentError, "请重新诊断。")
		return
	}
	err = wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeInvoke, ExecID: execID, Op: protocol.OpInfo, Payload: json.RawMessage(`{}`)})
	var content []byte
	for err == nil {
		err = wsjson.Read(ctx, c, &msg)
		if err != nil {
			break
		}
		if msg.ExecID != execID {
			continue
		}
		switch msg.Type {
		case protocol.TypeStdout:
			if len(content)+len(msg.Data) > 64*1024 {
				r.Set("target_runtime", "fail", "invalid_response", "目标返回异常，请检查目标版本。")
				return
			}
			content = append(content, msg.Data...)
		case protocol.TypeExit:
			var info struct {
				DeviceID string `json:"device_id"`
			}
			if msg.Status != protocol.ExitCompleted || msg.ExitCode == nil || *msg.ExitCode != 0 ||
				json.Unmarshal(content, &info) != nil || info.DeviceID != device {
				r.Set("target_runtime", "fail", "invalid_response", "目标返回异常，请检查目标版本。")
				return
			}
			for _, layer := range []string{"target_online", "inbound", "target_runtime"} {
				r.Set(layer, "ok", "", "")
			}
			r.OK = true
			return
		case protocol.TypeInvokeNack, protocol.TypeError:
			setFailure(r, msg.Code)
			return
		}
	}
	r.Set("target_runtime", "unknown", transportCode(err), "目标未完成应答；请检查目标服务，再重新诊断。")
}

func setFailure(r *Report, code string) {
	switch code {
	case protocol.Unauthorized:
		r.Set("authorization", "fail", code, "请对方重新签发授权码，再更新这台设备的配对信息。")
	case protocol.UnknownDevice:
		r.Set("target_online", "fail", code, "核对对方的 9 位设备 ID 和中转地址。")
	case protocol.DeviceOffline:
		r.Set("target_online", "fail", code, "请对方启动服务并检查网络。")
	case protocol.InboundDisabled:
		r.Set("target_online", "ok", "", "")
		r.Set("inbound", "fail", code, "请对方开启允许远程访问。")
	case protocol.QuotaExceeded:
		r.Set("target_runtime", "unknown", code, "中转繁忙，请稍后重新诊断。")
	default:
		r.Set("target_runtime", "unknown", "invalid_response", "中转或目标返回异常，请检查版本。")
	}
}

func transportCode(err error) string {
	var ne net.Error
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.As(err, &dns):
		return "dns_failed"
	case errors.As(err, &cert):
		return "tls_failed"
	default:
		return "connection_failed"
	}
}
