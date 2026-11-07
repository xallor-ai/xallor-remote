package relay

import (
	"context"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Every variable-sized field in the wire envelope must consume retained bytes,
// including nested metadata not meaningful for the received message type.
func TestMessageBudget_should_chargeEveryVariableField(t *testing.T) {
	var visit func(reflect.Type, []int, string)
	visit = func(typ reflect.Type, path []int, name string) {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Struct {
			for i := 0; i < typ.NumField(); i++ {
				next := append(append([]int{}, path...), i)
				visit(typ.Field(i).Type, next, name+"."+typ.Field(i).Name)
			}
			return
		}
		if typ.Kind() != reflect.String && !(typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8) {
			return
		}
		t.Run(name, func(t *testing.T) {
			var message protocol.Message
			value := reflect.ValueOf(&message).Elem()
			for _, index := range path {
				for value.Kind() == reflect.Pointer {
					if value.IsNil() {
						value.Set(reflect.New(value.Type().Elem()))
					}
					value = value.Elem()
				}
				value = value.Field(index)
			}
			for value.Kind() == reflect.Pointer {
				if value.IsNil() {
					value.Set(reflect.New(value.Type().Elem()))
				}
				value = value.Elem()
			}
			const size = 4096
			if value.Kind() == reflect.String {
				value.SetString(strings.Repeat("x", size))
			} else {
				value.SetBytes([]byte(strings.Repeat("x", size)))
			}
			if retainedBytes(message)-retainedBytes(protocol.Message{}) < size {
				t.Fatal("variable field bypassed retained-byte accounting")
			}
		})
	}
	visit(reflect.TypeOf(protocol.Message{}), nil, "message")
}

// The input reader must reject a second large control envelope before it can
// queue untrusted authentication metadata beyond its byte budget.
func TestClientAuth_should_consumeInboxBudget_beforeDispatch(t *testing.T) {
	inbox := &inbox{frames: make(chan inboundFrame, protocol.MaxRelayInboxFrames)}
	message := protocol.Message{Type: protocol.TypeHeartbeat, ClientAuth: &protocol.ClientAuthentication{Mode: strings.Repeat("x", protocol.MaxWSMessageBytes-protocol.MaxEnvelopeBytes)}}
	drop := func(protocol.Message) { t.Fatal("control frame silently dropped") }
	if !inbox.offer(message, drop) || inbox.offer(message, drop) {
		t.Fatal("client auth bypassed inbox byte budget")
	}
}

// Unused client-auth data in output must be dropped by the per-flow byte cap,
// while large control envelopes still consume the connection-wide budget.
func TestClientAuth_should_consumeOutboxBudgets_whenWriterStalls(t *testing.T) {
	for _, output := range []bool{true, false} {
		t.Run(map[bool]string{true: "output", false: "control"}[output], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			entered := make(chan struct{})
			var once sync.Once
			box := newOutbox(ctx, func(ctx context.Context, _ protocol.Message) error {
				once.Do(func() { close(entered) })
				<-ctx.Done()
				return ctx.Err()
			}, func() {})
			t.Cleanup(func() { cancel(); box.shutdown() })
			if err := box.Enqueue(protocol.Message{Type: protocol.TypeHelloOK}); err != nil {
				t.Fatal(err)
			}
			waitSignal(t, entered)
			message := protocol.Message{Type: protocol.TypeHeartbeat, ClientAuth: &protocol.ClientAuthentication{Mode: strings.Repeat("x", protocol.MaxWSMessageBytes-protocol.MaxEnvelopeBytes)}}
			if output {
				message.Type = protocol.TypeStdout
				message.ExecID = "flow"
				if err := box.Enqueue(message); err != errOutputDropped {
					t.Fatal("metadata output bypassed per-flow byte budget", err)
				}
			} else {
				if err := box.Enqueue(message); err != nil {
					t.Fatal(err)
				}
				if err := box.Enqueue(message); err != errOutboxClosed {
					t.Fatal("metadata control bypassed connection byte budget", err)
				}
			}
		})
	}
}
