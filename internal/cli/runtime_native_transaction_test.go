package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// Real owner IPC launches a separately authenticated transaction process which
// survives its parent's exit, commits the new Runtime, and records the outcome.
// The native gh fixture controls proof; this is not signed-release acceptance.
func TestNativeOwnerTransactionSurvivesServingProcessReplacement(t *testing.T) {
	profile, binary, first := nativeOwnerBundle(t)
	nextBundle, next := nativeCandidate(t, binary, first, "owner-transaction")
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error("runtime cleanup unconfirmed", err)
		}
	}()
	if err := s.Install(filepath.Dir(binary), runtimeControl{}); err != nil {
		t.Fatal(err)
	}
	before, err := (runtimeControl{}).Running()
	if err != nil || before == nil {
		t.Fatal(before, err)
	}
	identity := nativeIdentityDigest(t, profile)
	command, err := runtimeowner.NewCommand("install", nextBundle)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ipc.Dial(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(90 * time.Second))
	framed := ipc.NewConn(conn)
	namespace, err := (runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}).NamespaceID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ipc.Call(framed, "install", "runtime.install", map[string]string{"operation_id": command.ID, "bundle": command.Bundle, "namespace_sha256": namespace}); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	frame, replyErr := framed.ReadFrame()
	conn.Close()
	if replyErr == nil && (frame.OK == nil || !*frame.OK) {
		t.Logf("acceptance not confirmed: code=%s; observing original transaction", frame.Code)
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	deadline := time.Now().Add(120 * time.Second)
	observeCtx, cancelObservation := context.WithDeadline(context.Background(), deadline)
	defer cancelObservation()
	var receipt runtimeowner.Record
	err = waitNativeObservation(observeCtx, func() (bool, error) {
		receipt, err = journal.Query(command.ID)
		if errors.Is(err, runtimeowner.ErrUnknown) {
			return false, nil
		}
		return err == nil && receipt.Outcome != runtimeowner.Pending, err
	})
	if err != nil {
		t.Fatal("operation observation did not finish", replyErr, receipt, err)
	}
	if receipt.Outcome != runtimeowner.Succeeded {
		if log, err := os.ReadFile(filepath.Join(s.Dir, "transaction.log")); err == nil {
			t.Log(string(log))
		}
		t.Fatal("owner transaction failed", receipt)
	}
	// The terminal receipt must include the admitted worker's proof and query
	// groups; eventual lock release alone is not resource-settlement evidence.
	resources, err := journal.Resources(command.ID)
	if nativeReadBusy(err) {
		err = waitNativeObservation(observeCtx, func() (bool, error) {
			resources, err = journal.Resources(command.ID)
			return err == nil, err
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	workerGroups := 0
	for _, resource := range resources {
		if resource.Phase == runtimeowner.ResourcePhaseWorker {
			workerGroups++
			if resource.Status != runtimeowner.ResourceClosed {
				t.Fatal("worker resource not closed", resource)
			}
		}
	}
	if workerGroups != 2 {
		t.Fatal("missing worker resource groups", resources)
	}
	err = waitNativeObservation(observeCtx, func() (bool, error) {
		_, settled, queryErr := journal.QuerySettled(command.ID)
		return settled, queryErr
	})
	if err != nil {
		t.Fatal("worker terminal receipt did not settle", err)
	}
	after, err := (runtimeControl{}).Running()
	if err != nil || after == nil || after.PID == before.PID || after.SHA256 != next.Artifact.SHA256 {
		t.Fatal("replacement not observed", after, err)
	}
	if identity != nativeIdentityDigest(t, profile) {
		t.Fatal("replacement changed identity")
	}
	// The replacement owner exposes the same durable receipt over ordinary IPC.
	query, err := ipc.Dial(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close()
	query.SetDeadline(time.Now().Add(3 * time.Second))
	client := ipc.NewConn(query)
	if err := ipc.Call(client, "query", "runtime.operation.status", map[string]string{"operation_id": command.ID}); err != nil {
		t.Fatal(err)
	}
	response, err := client.ReadFrame()
	if err != nil || response.OK == nil || !*response.OK {
		t.Fatal(response, err)
	}
	var result struct {
		Operation runtimeowner.Record `json:"operation"`
	}
	if json.Unmarshal(response.Result, &result) != nil || result.Operation.Request.ID != command.ID || result.Operation.Outcome != runtimeowner.Succeeded {
		t.Fatal(response)
	}
}
