// A deliberately unconfirmed test Relay. It counts operations without storing
// tokens or payloads, so end-to-end clients can prove they never proceed.
package main

import (
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func main() {
	ready := flag.String("ready", "", "owned readiness file")
	flag.Parse()
	if *ready == "" {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	var shared, accounts, invokes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"shared": shared.Load(), "accounts": accounts.Load(), "invokes": invokes.Load()})
	})
	mux.HandleFunc("/remote", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		var message protocol.Message
		if wsjson.Read(r.Context(), c, &message) != nil {
			return
		}
		if message.Type != protocol.TypeHelloClient {
			_ = wsjson.Write(r.Context(), c, protocol.Message{Type: protocol.TypeError, Code: protocol.RelayError})
			return
		}
		if message.AccountToken == "" {
			shared.Add(1)
		} else {
			accounts.Add(1)
		}
		// Missing client_auth is intentional; never add a compatibility response.
		if wsjson.Write(r.Context(), c, protocol.Message{Type: protocol.TypeHelloOK, Role: protocol.RoleClient}) != nil {
			return
		}
		for wsjson.Read(r.Context(), c, &message) == nil {
			if message.Type != protocol.TypeInvoke {
				continue
			}
			invokes.Add(1)
			data := "unexpected-execution"
			if message.Op == protocol.OpInfo {
				data = `{"device_id":"123456789"}`
			}
			_ = wsjson.Write(r.Context(), c, protocol.Message{Type: protocol.TypeStdout, ExecID: message.ExecID, Data: data})
			_ = wsjson.Write(r.Context(), c, protocol.Message{Type: protocol.TypeExit, ExecID: message.ExecID, Status: protocol.ExitCompleted, ExitCode: protocol.Int(0), DurationMS: protocol.Int64(1), Truncated: protocol.Bool(false)})
		}
	})
	if err := os.WriteFile(*ready, []byte("http://"+listener.Addr().String()), 0600); err != nil {
		panic(err)
	}
	_ = http.Serve(listener, mux)
}
