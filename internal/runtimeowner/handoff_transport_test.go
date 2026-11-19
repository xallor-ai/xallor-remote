package runtimeowner

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Only one bounded, strictly decoded envelope is accepted; extra fields or
// trailing messages cannot change the operation after its intent is checked.
func TestDecodeHandoffRejectsUnknownFieldsAndTrailingMessages(t *testing.T) {
	command := Command{ID: strings.Repeat("1", 32), Kind: "install", Bundle: t.TempDir()}
	r, _ := command.Request()
	value := Handoff{Command: command, Permit: release.OwnerHandoff{OperationID: r.ID, IntentSHA256: r.IntentSHA256}}
	raw, _ := json.Marshal(value)
	if _, err := DecodeHandoff(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{append(append([]byte(nil), raw...), []byte(` {}`)...), []byte(`{"command":{},"permit":{},"extra":1}`), bytes.Repeat([]byte(" "), MaxHandoffBytes+1)} {
		if _, err := DecodeHandoff(bytes.NewReader(invalid)); err == nil {
			t.Fatal("invalid handoff accepted")
		}
	}
}

// The fixed transaction endpoint answers while the ordinary owner endpoint is
// absent and closes idle readers before shutdown completes.
func TestTransactionQueryEndpointSurvivesServingEndpointAbsence(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	j := Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	ln, err := ipc.ListenLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeQueries(ctx, ln, j) }()
	conn, err := ipc.DialLifecycle(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	client := ipc.NewConn(conn)
	if err := ipc.Call(client, "query", "runtime.operation.status", map[string]string{"operation_id": request("1").ID}); err != nil {
		t.Fatal(err)
	}
	frame, err := client.ReadFrame()
	if err != nil || frame.OK == nil || !*frame.OK {
		t.Fatal(frame, err)
	}
	idle, err := ipc.DialLifecycle(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("query shutdown retained idle connection")
	}
}
