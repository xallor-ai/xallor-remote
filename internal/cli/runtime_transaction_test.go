package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// An ordinary head cannot launch a worker or keep it waiting on an open input
// pipe. Parent/endpoint authentication happens before reading that input.
func TestRuntimeTransactionRejectsOrdinaryParentBeforeReadingInput(t *testing.T) {
	profile, binary, _ := nativeOwnerBundle(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "runtime-transaction")
	configureDetached(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if output, err := cmd.CombinedOutput(); err == nil || ctx.Err() != nil {
		t.Fatalf("worker did not reject ordinary parent: %v %v %s", err, ctx.Err(), output)
	}
	if _, err := os.Stat(filepath.Join(profile, "runtime", "lifecycle")); !os.IsNotExist(err) {
		t.Fatal("rejected worker wrote journal", err)
	}
}

// The ordinary internal service launch uses the same finite inherited-input
// boundary; a writer holding an empty pipe cannot pin a service process forever.
func TestRuntimeServeBoundsOpenInheritedInput(t *testing.T) {
	profile, binary, _ := nativeOwnerBundle(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "runtime-serve")
	configureDetached(cmd)
	writer, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	output, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "deadline exceeded") {
		t.Fatal("service entry did not bound inherited input", err, ctx.Err(), string(output))
	}
	if _, err := os.Stat(filepath.Join(profile, "identity.db")); !os.IsNotExist(err) {
		t.Fatal("empty input initialized identity", err)
	}
}

// A genuine parent/peer PID with spoofed image metadata is still rejected by
// kernel image observation. No input is supplied, proving rejection precedes it.
func TestRuntimeTransactionRejectsSpoofedParentImageBeforeInput(t *testing.T) {
	profile, binary, manifest := nativeOwnerBundle(t)
	bundle, candidate := nativeCandidate(t, binary, manifest, "different-parent-image")
	binary = filepath.Join(bundle, candidate.Artifact.Filename)
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := ipc.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	served := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			served <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		framed := ipc.NewConn(conn)
		req, err := framed.ReadRequest()
		if err != nil {
			served <- err
			return
		}
		claimed := buildinfo.Executable{Info: candidate.Build, Path: binary, SHA256: candidate.Artifact.SHA256, PID: os.Getpid(), OwnerProtocol: buildinfo.RuntimeOwnerProtocol}
		served <- framed.WriteFrame(ipc.OK(req.ID, map[string]any{"build": claimed}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "runtime-transaction")
	configureDetached(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if output, err := cmd.CombinedOutput(); err == nil || ctx.Err() != nil {
		t.Fatalf("spoofed parent not rejected: %v %v %s", err, ctx.Err(), output)
	} else if !strings.Contains(string(output), "actual process image digest differs") && !strings.Contains(string(output), "unsuccessful") {
		t.Fatalf("rejection did not verify the parent image: %s", output)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, "runtime", "lifecycle")); !os.IsNotExist(err) {
		t.Fatal("spoofed parent wrote journal", err)
	}
}
