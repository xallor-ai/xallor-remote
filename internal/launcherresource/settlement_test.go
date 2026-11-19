package launcherresource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

type syntheticClose struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	err     error
}

// Even after native pipe cancellation, the observer must finish its own return
// path before Done. A held reader-return barrier models scheduled teardown.
func TestAttempt_should_joinObserver_beforeDone(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	owned := Own(input)
	entered := make(chan struct{})
	releaseRead := make(chan struct{})
	a, _ := Start(context.Background(), func() error { return nil }, func(ctx context.Context) error {
		defer owned.Close()
		reader, err := NewPipeReader(ctx, input)
		if err != nil {
			return err
		}
		_, err = reader.Read(make([]byte, 1))
		close(entered)
		<-releaseRead
		return err
	}, owned)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop on exit")
	}
	select {
	case <-a.Done():
		t.Fatal("Done before observer returned")
	default:
	}
	close(releaseRead)
	if err := a.Result(); err != nil {
		t.Fatal(err)
	}
}

// Resources acquired before a later preparation failure still need settlement;
// closing the set again preserves first failures without skipping later owners.
func TestResources_should_closeEveryPartialResource_andRetainFirstFailure(t *testing.T) {
	failure := errors.New("synthetic partial close failure")
	first := &syntheticClose{entered: make(chan struct{}), release: make(chan struct{}), err: failure}
	second := &syntheticClose{entered: make(chan struct{}), release: make(chan struct{})}
	close(first.release)
	close(second.release)
	resources := []*Owned{Own(first), Own(second)}
	for range 2 {
		if err := CloseAll(resources...); !errors.Is(err, failure) || !errors.Is(err, release.ErrCleanupUncertain) {
			t.Fatal(err)
		}
	}
	if first.calls.Load() != 1 || second.calls.Load() != 1 {
		t.Fatal("partial resources not closed exactly once")
	}
}

func (c *syntheticClose) Close() error { c.calls.Add(1); close(c.entered); <-c.release; return c.err }

// Synthetic fault at the owned io.Closer boundary: a real process exits while
// its owned close is blocked. Done must wait and retain the first failed close.
func TestAttempt_should_waitForOwnedClose_andCacheFirstFailure(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic owned close failure")
	c := &syntheticClose{entered: make(chan struct{}), release: make(chan struct{}), err: failure}
	resource := Own(c)
	a, _ := Start(context.Background(), cmd.Wait, func(context.Context) error { return nil }, resource)
	select {
	case <-c.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("close not reached")
	}
	select {
	case <-a.Done():
		t.Fatal("Done before close returned")
	default:
	}
	close(c.release)
	if err := a.Result(); !errors.Is(err, failure) || !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
	if err := resource.Close(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if c.calls.Load() != 1 {
		t.Fatal("retried failed close")
	}
}

// A real open writer remains alive after valid JSON; acknowledgement must
// return immediately without EOF. The attempt still retains its live process.
func TestAttempt_should_acceptCompleteJSON_withoutWaitingForEOF(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	owned := Own(input)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exited := make(chan struct{})
	a, ack := Start(ctx, func() error { <-exited; return nil }, func(ctx context.Context) error {
		defer owned.Close()
		reader, err := NewPipeReader(ctx, input)
		if err != nil {
			return err
		}
		var got struct {
			Accepted bool `json:"accepted"`
		}
		return json.NewDecoder(io.LimitReader(reader, 4097)).Decode(&got)
	}, owned)
	if _, err := writer.Write([]byte(`{"accepted":true}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ack:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("JSON waited for EOF")
	}
	select {
	case <-a.Done():
		t.Fatal("completed before process exit")
	default:
	}
	close(exited)
	if err := a.Result(); err != nil {
		t.Fatal(err)
	}
}

// Cancellation terminates an actual pipe read and joins its resource closure,
// but it must not report process completion before the separately owned exit.
func TestAttempt_should_retainProcess_afterObservationCancellation(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	owned := Own(input)
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan struct{})
	readReturned := make(chan struct{})
	a, ack := Start(ctx, func() error { <-exited; return nil }, func(ctx context.Context) error {
		defer close(readReturned)
		defer owned.Close()
		reader, err := NewPipeReader(ctx, input)
		if err != nil {
			return err
		}
		_, err = reader.Read(make([]byte, 1))
		return err
	}, owned)
	cancel()
	select {
	case err := <-ack:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader pinned by open writer")
	}
	<-readReturned
	select {
	case <-a.Done():
		t.Fatal("observation cancellation completed live process")
	default:
	}
	close(exited)
	if err := a.Result(); err != nil {
		t.Fatal(err)
	}
}

// A process that fails cannot prove its admission cleanup even when all parent
// resources close successfully; its attempt must retain cleanup uncertainty.
func TestAttempt_should_retainUncertainty_whenProcessWaitFails(t *testing.T) {
	failure := errors.New("worker exit failure")
	a, _ := Start(context.Background(), func() error { return failure }, func(context.Context) error { return nil })
	if err := a.Result(); !errors.Is(err, failure) || !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
}
