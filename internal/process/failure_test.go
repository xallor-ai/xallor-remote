package process

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

type failedSettlement struct{ boundary }

func (g failedSettlement) settle() error {
	if err := g.boundary.settle(); err != nil {
		return err
	}
	return errors.New("injected settlement uncertainty")
}

// A successful process exit cannot conceal an uncertain group cleanup result.
// The real group is still stopped, so this fault injection leaves no live child.
func TestProcess_should_reportCleanupUncertainty_whenRootSucceeded(t *testing.T) {
	cmd := helper("empty")
	g, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	child, err := startPrepared(context.Background(), cmd, failedSettlement{g})
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); !errors.Is(err, ErrCleanup) {
		t.Fatal("cleanup failure lost", err)
	}
	if !cmd.ProcessState.Success() {
		t.Fatal("fixture did not exit successfully")
	}
}

type failedAttachment struct {
	boundary
	closed bool
}

func (g *failedAttachment) attach(cmd *exec.Cmd) error { return errors.New("injected attach failure") }
func (g *failedAttachment) close() error               { g.closed = true; return g.boundary.close() }

// Failed supervision must kill/reap the root and close the boundary rather than
// silently switching to unsupervised execution or returning a usable Child.
func TestProcess_should_reapRoot_whenAttachmentFails(t *testing.T) {
	cmd := helper("wait")
	g, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	fault := &failedAttachment{boundary: g}
	child, err := startPrepared(context.Background(), cmd, fault)
	if err == nil || child != nil || cmd.ProcessState == nil || !fault.closed {
		t.Fatal("failed start leaked ownership", err)
	}
}
