//go:build linux

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type acceptanceTestLease struct{}

func (acceptanceTestLease) Check() error { return nil }
func (acceptanceTestLease) Close() error { return nil }

// The parent drops the actual stdout pipe after durable admission. The child
// must survive EPIPE, execute its missing-source backend, and settle the receipt.
func TestWorkerShouldSurviveClosedNativeAcceptancePipe(t *testing.T) {
	const childFlag = "XR_TEST_CLOSED_ACCEPTANCE_PROFILE"
	if profile := os.Getenv(childFlag); profile != "" {
		command := runtimeowner.Command{ID: strings.Repeat("a", 32), Kind: "install", Bundle: filepath.Join(profile, "missing-source")}
		req, _ := command.Request()
		raw, _ := runtimeowner.EncodeHandoff(runtimeowner.Handoff{Command: command, Permit: release.OwnerHandoff{OperationID: req.ID, IntentSHA256: req.IntentSHA256}})
		worker := runtimeowner.Worker{Journal: runtimeowner.Journal{Dir: filepath.Join(profile, "lifecycle")}, Store: release.Store{Dir: profile}, Control: runtimeControl{}, Authenticate: func(context.Context, runtimeowner.Handoff) (runtimeowner.Admission, error) {
			return acceptanceTestLease{}, nil
		}, Listen: func() (net.Listener, error) {
			if e := os.WriteFile(filepath.Join(profile, "admitted"), nil, 0600); e != nil {
				return nil, e
			}
			var barrier [1]byte
			if _, e := os.Stdin.Read(barrier[:]); e != nil {
				return nil, e
			}
			return net.Listen("tcp", "127.0.0.1:0")
		}}
		err := worker.Run(context.Background(), bytes.NewReader(raw), transactionAcceptanceWriter{file: os.Stdout})
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "backend did not execute", err)
			os.Exit(2)
		}
		// Exit without testing's own stdout summary writing to the intentionally
		// broken pipe: only the real acceptance write is under test.
		os.Exit(0)
	}
	profile := t.TempDir()
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ackRead, ackWrite, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	barrierRead, barrierWrite, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer barrierRead.Close()
	defer barrierWrite.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestWorkerShouldSurviveClosedNativeAcceptancePipe$")
	cmd.Env = append(os.Environ(), childFlag+"="+profile)
	cmd.Stdin = barrierRead
	cmd.Stdout = ackWrite
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	ackWrite.Close()
	barrierRead.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(profile, "admitted")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			cmd.Wait()
			t.Fatal("admission barrier absent", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	ackRead.Close()
	if _, e := barrierWrite.Write([]byte{1}); e != nil {
		cancel()
		cmd.Wait()
		t.Fatal(e)
	}
	exitErr := cmd.Wait()
	journal := runtimeowner.Journal{Dir: filepath.Join(profile, "lifecycle")}
	r, settled, e := journal.QuerySettled(strings.Repeat("a", 32))
	if exitErr != nil || e != nil || !settled || r.Outcome != runtimeowner.Rejected {
		t.Fatalf("exit=%v outcome=%s settled=%v query=%v stderr=%s", exitErr, r.Outcome, settled, e, stderr.String())
	}
	rows, e := journal.Resources(r.Request.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 {
		t.Fatal("worker rows", rows)
	}
	for _, row := range rows {
		if row.Status != runtimeowner.ResourceClosed {
			t.Fatal(row)
		}
	}
}
