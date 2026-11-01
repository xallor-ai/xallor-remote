package noderun

import (
	"context"
	"encoding/json"
	"log/slog"
	stdruntime "runtime"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type wsConn struct {
	mu   sync.Mutex
	ctx  context.Context
	conn *websocket.Conn
}

func (w *wsConn) send(m protocol.Message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(raw) > protocol.MaxWSMessageBytes {
		return errMsgTooLarge
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.Write(w.ctx, websocket.MessageText, raw)
}

var errMsgTooLarge = errStr(protocol.TooLarge)

type clientLink struct {
	target  string
	ws      *wsConn
	cancel  context.CancelFunc
	mu      sync.Mutex
	waiters map[string]*execWaiter
}

type Daemon struct {
	log    *slog.Logger
	store  *identity.Store
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	device    *wsConn
	clients   map[string]*clientLink
	ensuring  map[string]chan struct{}
	local     map[string]context.CancelFunc
	remote    map[string]*remoteExec
	approvals *approvalHub
}

type remoteExec struct {
	link *wsConn
	ipc  *ipc.Conn
}

func New(store *identity.Store, log *slog.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	return &Daemon{
		log: log, store: store, ctx: ctx, cancel: cancel,
		clients:   map[string]*clientLink{},
		ensuring:  map[string]chan struct{}{},
		local:     map[string]context.CancelFunc{},
		remote:    map[string]*remoteExec{},
		approvals: newApprovalHub(),
	}
}

func (d *Daemon) Stop() { d.cancel() }

func (d *Daemon) Run() error {
	go d.keepDevice()
	ln, err := ipc.Listen()
	if err != nil {
		return err
	}
	defer ln.Close()
	d.log.Info("runtime ipc up", "device", d.store.DeviceID)
	for {
		c, err := ln.Accept()
		if err != nil {
			if d.ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.serveIPC(ipc.NewConn(c))
	}
}

func (d *Daemon) keepDevice() {
	backoff := time.Second
	for d.ctx.Err() == nil {
		if err := d.connectDevice(); err != nil {
			d.log.Warn("device reconnect", "err", err)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (d *Daemon) connectDevice() error {
	id, secret, grant, ws, relay, inbound, _ := d.store.Snapshot()
	ctx, cancel := context.WithCancel(d.ctx)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, relay, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	defer conn.Close(websocket.StatusNormalClosure, "")
	link := &wsConn{ctx: ctx, conn: conn}
	inb := inbound && grant != ""
	hello := protocol.Message{
		Type: protocol.TypeHelloDevice, DeviceID: id, Secret: secret,
		OS: stdruntime.GOOS, Arch: stdruntime.GOARCH, AgentVersion: protocol.Version,
		Workspace: ws, Inbound: protocol.Bool(inb),
	}
	if err := link.send(hello); err != nil {
		return err
	}
	var ack protocol.Message
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		return err
	}
	if ack.Type != protocol.TypeHelloOK {
		return errHello(ack.Code)
	}
	if grant != "" {
		_ = link.send(protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex(grant)})
		_ = link.send(protocol.Message{Type: protocol.TypeInboundSet, Inbound: protocol.Bool(true)})
	}
	d.mu.Lock()
	d.device = link
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		if d.device == link {
			d.device = nil
		}
		d.mu.Unlock()
		d.killAllLocal()
	}()

	t := time.NewTicker(protocol.HeartbeatEvery)
	defer t.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = link.send(protocol.Message{Type: protocol.TypeHeartbeat})
			}
		}
	}()
	for {
		var msg protocol.Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return err
		}
		d.onDeviceMsg(link, msg)
	}
}

func errHello(code string) error {
	return errStr("hello: " + code)
}

type errStr string

func (e errStr) Error() string { return string(e) }

func (d *Daemon) onDeviceMsg(link *wsConn, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeHeartbeat:
	case protocol.TypeInvoke:
		if msg.Op == protocol.OpCancel {
			d.approvals.cancelExec(msg.ExecID)
			d.mu.Lock()
			cancel := d.local[msg.ExecID]
			d.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			return
		}
		switch msg.Op {
		case protocol.OpExec:
			go d.runLocalExec(link, msg)
		case protocol.OpRead:
			go d.runLocalRead(link, msg)
		case protocol.OpWrite:
			go d.runLocalWrite(link, msg)
		case protocol.OpProcesses:
			go d.runLocalProcesses(link, msg)
		case protocol.OpInfo:
			go d.runLocalInfo(link, msg)
		}
	}
}

func (d *Daemon) dropClient(id string, inst *clientLink) {
	d.mu.Lock()
	cur := d.clients[id]
	if inst != nil && cur != inst {
		d.mu.Unlock()
		inst.closeConn()
		return
	}
	delete(d.clients, id)
	d.mu.Unlock()
	if cur != nil {
		cur.closeConn()
	}
}

func (d *Daemon) deviceOnline() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.device != nil
}

func (d *Daemon) pushGrant(grant string) {
	d.mu.Lock()
	link := d.device
	d.mu.Unlock()
	if link == nil {
		return
	}
	_ = link.send(protocol.Message{Type: protocol.TypeGrantRotate, NewGrantHash: protocol.SHA256Hex(grant)})
	_ = link.send(protocol.Message{Type: protocol.TypeInboundSet, Inbound: protocol.Bool(true)})
}

func (d *Daemon) killAllLocal() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.local {
		c()
	}
}
