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

func Serve(addr, dataDir string, log *slog.Logger, quota Quota) error {
	store, err := OpenSQLStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	hub := NewHub(store)
	var lim *Limiter
	if quota.Enabled {
		lim = NewLimiter(quota)
		log.Info("relay quota on")
	}
	log.Info("relay listen", "addr", addr, "data", dataDir)
	srv := &http.Server{Addr: addr, Handler: HandlerQuota(hub, log, lim), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

func Handler(hub *Hub, log *slog.Logger) http.Handler {
	return HandlerQuota(hub, log, nil)
}

func HandlerQuota(hub *Hub, log *slog.Logger, lim *Limiter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", writeHealth)
	mux.HandleFunc("/remote/health", writeHealth)
	mux.HandleFunc("/xr/health", writeHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			if log != nil {
				log.Warn("accept", "err", err)
			}
			return
		}
		go serveConn(context.Background(), hub, c, log, lim, ClientIP(r))
	})
	return mux
}

func serveConn(parent context.Context, hub *Hub, c *websocket.Conn, log *slog.Logger, lim *Limiter, ip string) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s := newOutbox(ctx, func(writeCtx context.Context, m protocol.Message) error {
		return wsjson.Write(writeCtx, c, m)
	}, func() { _ = c.CloseNow() })
	defer s.shutdown()

	c.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	var hello protocol.Message
	ctxRead, cancelRead := context.WithTimeout(ctx, 15*time.Second)
	err := wsjson.Read(ctxRead, c, &hello)
	cancelRead()
	if err != nil {
		return
	}

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
		inb := false
		if hello.Inbound != nil {
			inb = *hello.Inbound
		}
		if code := hub.RegisterDevice(hello.DeviceID, hello.Secret, inb, s); code != "" {
			_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
			return
		}
		lim.OnDeviceUp(ip)
		defer lim.OnDeviceDown(ip)
		role, bindID = protocol.RoleDevice, hello.DeviceID
	case protocol.TypeHelloClient:
		if hello.TargetID == "" || hello.Grant == "" {
			return
		}
		if code := hub.RegisterClient(hello.TargetID, hello.Grant, s); code != "" {
			_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, Code: code})
			return
		}
		role, bindID = protocol.RoleClient, hello.TargetID
	default:
		return
	}

	defer hub.Drop(s, role, bindID)

	for {
		ctxRead, cancelRead = context.WithTimeout(ctx, protocol.ConnStaleAfter)
		var msg protocol.Message
		err := wsjson.Read(ctxRead, c, &msg)
		cancelRead()
		if err != nil {
			return
		}
		if b, e := msg.Bytes(); e == nil {
			if code := lim.AddBytes(ip, len(b)); code != "" {
				_ = s.Enqueue(protocol.Message{Type: protocol.TypeError, ExecID: msg.ExecID, Code: code})
				return
			}
		}
		hub.Handle(s, role, bindID, msg)
	}
}
