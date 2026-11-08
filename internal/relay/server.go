package relay

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func Handler(hub *Hub, log *slog.Logger) http.Handler {
	return HandlerQuota(hub, log, nil)
}

func HandlerQuota(hub *Hub, log *slog.Logger, lim *Limiter) http.Handler {
	return handlerPolicy(hub, log, lim, requestPolicy{})
}

func handlerPolicy(hub *Hub, log *slog.Logger, lim *Limiter, policy requestPolicy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", writeHealth)
	mux.HandleFunc("/remote/health", writeHealth)
	mux.HandleFunc("/xr/health", writeHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !policy.allowOrigin(r) {
			if log != nil {
				log.Warn("relay request denied", "client_ip", policy.clientIP(r), "reason", "origin")
			}
			http.Error(w, "Origin is not allowed", http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originPatterns(policy)})
		if err != nil {
			if log != nil {
				log.Warn("accept", "err", err)
			}
			return
		}
		go serveConn(context.Background(), hub, c, log, lim, policy.clientIP(r))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !policy.allowTransport(r) {
			if log != nil {
				log.Warn("relay request denied", "client_ip", policy.clientIP(r), "reason", "transport")
			}
			http.Error(w, "A trusted HTTPS proxy is required", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func serveConn(parent context.Context, hub *Hub, c *websocket.Conn, log *slog.Logger, lim *Limiter, ip string) {
	ctx, cancel := context.WithCancel(parent)
	s := newOutbox(ctx, func(writeCtx context.Context, m protocol.Message) error {
		return wsjson.Write(writeCtx, c, m)
	}, func() { _ = c.CloseNow() })
	var incoming *inbox
	defer func() {
		s.shutdown()
		cancel()
		if incoming != nil {
			<-incoming.done
		}
	}()

	c.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	var hello protocol.Message
	ctxRead, cancelRead := context.WithTimeout(ctx, 15*time.Second)
	err := wsjson.Read(ctxRead, c, &hello)
	cancelRead()
	if err != nil {
		return
	}
	incoming = newInbox(ctx, c, cancel, func(m protocol.Message) bool {
		// Charge received traffic before queue admission, including discarded output.
		if b, e := m.Bytes(); e == nil {
			if code := lim.AddBytes(ip, len(b)); code != "" {
				_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, ExecID: m.ExecID, Code: code})
				s.shutdown()
				return false
			}
		}
		return true
	}, func(m protocol.Message) { hub.noteDroppedOutput(s, m) })

	var role, bindID string
	switch hello.Type {
	case protocol.TypeHelloDevice:
		if hello.DeviceID == "" || hello.Secret == "" {
			return
		}
		if code := lim.AllowDeviceHello(ip); code != "" {
			_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
			return
		}
		// Hello proves identity only. Initial access stays disabled until the
		// complete settings snapshot is committed and acknowledged.
		if code := hub.RegisterDevice(hello.DeviceID, hello.Secret, false, s); code != "" {
			_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
			return
		}
		lim.OnDeviceUp(ip)
		defer lim.OnDeviceDown(ip)
		role, bindID = protocol.RoleDevice, hello.DeviceID
	case protocol.TypeHelloClient:
		if hello.TargetID == "" {
			return
		}
		if code := hub.RegisterClient(hello.TargetID, hello.Grant, hello.AccountToken, s); code != "" {
			_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
			return
		}
		role, bindID = protocol.RoleClient, hello.TargetID
	default:
		return
	}

	defer hub.Drop(s, role, bindID)

	for frame := range incoming.frames {
		incoming.bytes.Add(-frame.bytes)
		msg := frame.message
		hub.Handle(s, role, bindID, msg)
	}
}
