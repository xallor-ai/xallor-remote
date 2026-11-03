package noderun

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type clientKey struct {
	relay, target, grantHash, credentialHash string
	generation                               uint64
}

func (d *Daemon) runClientReader(deviceID string, cl *clientLink) {
	for {
		var msg protocol.Message
		if err := wsjson.Read(cl.ws.ctx, cl.ws.conn, &msg); err != nil {
			cl.failWaiters()
			d.dropClient(deviceID, cl)
			return
		}
		cl.dispatch(msg)
	}
}

func (d *Daemon) ensureClient(peer identity.Peer) (*wsConn, *clientLink, error) {
	for {
		d.mu.Lock()
		relay, token, err := d.store.Credentials()
		if err != nil {
			d.mu.Unlock()
			return nil, nil, err
		}
		key := clientKey{relay: relay, target: peer.DeviceID, grantHash: protocol.SHA256Hex(peer.Grant), credentialHash: protocol.SHA256Hex(token), generation: d.accountGeneration}
		var stale []*clientLink
		for existing, cl := range d.clients {
			if existing.target == key.target && existing != key {
				delete(d.clients, existing)
				stale = append(stale, cl)
			}
		}
		if len(stale) > 0 {
			d.mu.Unlock()
			for _, cl := range stale {
				cl.closeConn()
			}
			continue
		}
		if cl, ok := d.clients[key]; ok && cl.ws != nil {
			d.mu.Unlock()
			return cl.ws, cl, nil
		}
		if wait, ok := d.ensuring[key]; ok {
			d.mu.Unlock()
			select {
			case <-wait:
			case <-d.ctx.Done():
				return nil, nil, d.ctx.Err()
			}
			continue
		}
		done := make(chan struct{})
		d.ensuring[key] = done
		d.mu.Unlock()

		link, cl, err := d.dialClient(peer, key, token)
		d.mu.Lock()
		delete(d.ensuring, key)
		currentRelay, currentToken, currentErr := d.store.Credentials()
		if err == nil && (currentErr != nil || key.generation != d.accountGeneration || currentRelay != key.relay || protocol.SHA256Hex(currentToken) != key.credentialHash || link.ctx.Err() != nil) {
			err = fmt.Errorf("身份或连接已变更，请重试")
		}
		if err == nil {
			d.clients[key] = cl
		}
		close(done)
		d.mu.Unlock()
		if err != nil && cl != nil {
			cl.closeConn()
			return nil, nil, err
		}
		return link, cl, err
	}
}

func (d *Daemon) dialClient(peer identity.Peer, key clientKey, token string) (*wsConn, *clientLink, error) {
	ctx, cancel := context.WithCancel(d.ctx)
	helloCtx, stopHello := context.WithTimeout(ctx, protocol.RelayIdentityTimeout)
	defer stopHello()
	conn, _, err := websocket.Dial(helloCtx, key.relay, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			cancel()
			_ = conn.CloseNow()
		}
	}()
	conn.SetReadLimit(int64(protocol.MaxWSMessageBytes))
	link := &wsConn{ctx: ctx, conn: conn}
	hello := protocol.Message{Type: protocol.TypeHelloClient, TargetID: peer.DeviceID, Grant: peer.Grant, AccountToken: token}
	if err := wsjson.Write(helloCtx, conn, hello); err != nil {
		cancel()
		return nil, nil, err
	}
	var ack protocol.Message
	if err := wsjson.Read(helloCtx, conn, &ack); err != nil {
		cancel()
		return nil, nil, err
	}
	if ack.Type != protocol.TypeHelloOK {
		cancel()
		return nil, nil, fmt.Errorf("%s", ack.Code)
	}
	if !ack.ConfirmsClientAuth(token != "") {
		return nil, nil, fmt.Errorf("中转未确认连接认证，请检查版本。")
	}
	cl := &clientLink{target: peer.DeviceID, ws: link, cancel: cancel, waiters: map[string]*execWaiter{}}
	success = true
	go d.runClientReader(peer.DeviceID, cl)
	go func() {
		t := time.NewTicker(protocol.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-d.ctx.Done():
				d.dropClient(peer.DeviceID, cl)
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := link.send(protocol.Message{Type: protocol.TypeHeartbeat}); err != nil {
					d.dropClient(peer.DeviceID, cl)
					return
				}
			}
		}
	}()
	return link, cl, nil
}
