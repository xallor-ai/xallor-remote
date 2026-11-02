package noderun

import (
	"context"
	"log/slog"
	stdruntime "runtime"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/xallor-ai/xallor-remote/internal/activity"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/handoff"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

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

	mu                sync.Mutex
	device            *wsConn
	clients           map[clientKey]*clientLink
	ensuring          map[clientKey]chan struct{}
	accountGeneration uint64
	accessRequests    deviceReplies
	settingsReplies   deviceReplies
	settings          settingsGate
	local             map[string]context.CancelFunc
	localWG           sync.WaitGroup
	remote            map[string]*remoteExec
	approvals         *approvalHub
	activity          *activity.Log
	handoffs          *handoff.Manager
	jobs              *job.Ledger
}

type remoteExec struct {
	mu   sync.Mutex // serialize invoke with cancellation; never hold Daemon.mu during I/O
	sent bool
	link *wsConn
	ipc  *ipc.Conn
}

func New(store *identity.Store, log *slog.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{
		log: log, store: store, ctx: ctx, cancel: cancel,
		clients:   map[clientKey]*clientLink{},
		ensuring:  map[clientKey]chan struct{}{},
		local:     map[string]context.CancelFunc{},
		remote:    map[string]*remoteExec{},
		approvals: newApprovalHub(),
		activity:  activity.Open(store.Dir),
		jobs:      job.New(store.Dir),
	}
	d.handoffs = handoff.New(ctx, store.Dir, d.jobs)
	return d
}

func (d *Daemon) Stop() { d.cancel() }

func (d *Daemon) Run() error {
	defer d.jobs.Close()
	defer d.handoffs.Close()
	defer d.waitLocalExecs()
	_ = buildinfo.Running()
	ln, err := ipc.Listen()
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() { <-d.ctx.Done(); _ = ln.Close() }()
	go d.keepDevice()
	d.log.Info("runtime ipc up", "device", d.store.ID())
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
	setup, setupCancel := context.WithTimeout(d.ctx, protocol.RelayIdentityTimeout)
	defer setupCancel()
	release, err := d.settings.acquire(setup)
	if err != nil {
		return err
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	if err := d.store.RefreshDevice(); err != nil {
		return err
	}
	id, secret, grant, ws, relay, inbound, _ := d.store.Snapshot()
	ctx, cancel := context.WithCancel(d.ctx)
	defer cancel()
	conn, _, err := websocket.Dial(setup, relay, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	defer conn.Close(websocket.StatusNormalClosure, "")
	link := &wsConn{ctx: ctx, conn: conn, relay: relay, observe: func(msg protocol.Message) { d.recordTerminal("inbound", id, msg) }}
	inb := inbound && grant != ""
	hello := protocol.Message{
		Type: protocol.TypeHelloDevice, DeviceID: id, Secret: secret,
		OS: stdruntime.GOOS, Arch: stdruntime.GOARCH, AgentVersion: buildinfo.Version(),
		Workspace: ws,
	}
	if err := link.sendContext(setup, hello); err != nil {
		return err
	}
	var ack protocol.Message
	if err := wsjson.Read(setup, conn, &ack); err != nil {
		return err
	}
	if ack.Type != protocol.TypeHelloOK || ack.Role != protocol.RoleDevice {
		return errHello(ack.Code)
	}
	settings := settingsMessage(grant, inb)
	settings.RequestID, err = protocol.RandomHex(16)
	if err != nil {
		return err
	}
	if err := link.sendContext(setup, settings); err != nil {
		return err
	}
	if err := wsjson.Read(setup, conn, &ack); err != nil {
		return err
	}
	if ack.Type != protocol.TypeDeviceSettingsResult || ack.RequestID != settings.RequestID || ack.Code != "" {
		return errStr("device settings unconfirmed")
	}
	d.mu.Lock()
	d.device = link
	d.mu.Unlock()
	release()
	release = nil
	setupCancel()
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
	case protocol.TypeDeviceSettingsResult:
		d.settingsReplies.deliver(link, msg)
	case protocol.TypeAccessResult:
		d.accessRequests.deliver(link, msg)
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
		d.recordActivity(activity.Entry{ExecID: msg.ExecID, Direction: "inbound", DeviceID: d.store.ID(), Op: msg.Op, Stage: "requested"})
		if d.handleHandoffInvoke(link, msg) {
			return
		}
		switch msg.Op {
		case protocol.OpExec:
			d.startLocalExec(link, msg)
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
	var removed []*clientLink
	for key, cl := range d.clients {
		if key.target == id && (inst == nil || cl == inst) {
			delete(d.clients, key)
			removed = append(removed, cl)
		}
	}
	d.mu.Unlock()
	for _, cl := range removed {
		cl.closeConn()
	}
	if inst != nil && len(removed) == 0 {
		inst.closeConn()
	}
}

func (d *Daemon) deviceOnline() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.device != nil
}

func (d *Daemon) killAllLocal() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.local {
		c()
	}
}
