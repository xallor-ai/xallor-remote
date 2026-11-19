package runtimeowner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Admission retains the authenticated parent identity until durable handoff.
// Check is repeated under the journal lock; Close must release its proof resources.
type Admission interface {
	Check() error
	Close() error
}

type Acceptance struct {
	Accepted     bool   `json:"accepted"`
	OperationID  string `json:"operation_id"`
	IntentSHA256 string `json:"intent_sha256"`
	PID          int    `json:"pid"`
}

type Worker struct {
	Journal      Journal
	Store        release.Store
	Control      release.Control
	Authenticate func(context.Context, Handoff) (Admission, error)
	Listen       func() (net.Listener, error)
}

// Run uses ctx only for admission. After durable acceptance, losing the original
// owner/head reply transport cannot cancel or replay the requested installation.
func (w Worker) Run(ctx context.Context, input io.Reader, ack io.Writer) (result error) {
	if input == nil || ack == nil || w.Control == nil || w.Authenticate == nil || w.Listen == nil ||
		filepath.Clean(w.Journal.Dir) != filepath.Join(filepath.Clean(w.Store.Dir), "lifecycle") {
		return ErrInvalid
	}
	handoff, err := DecodeHandoff(input)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, err := w.Authenticate(ctx, handoff)
	closed := false
	closeLease := func() error {
		if closed || lease == nil {
			return nil
		}
		closed = true
		if err := lease.Close(); err != nil {
			return errors.Join(release.ErrCleanupUncertain, err)
		}
		return nil
	}
	if lease != nil {
		defer func() {
			if !closed {
				result = errors.Join(result, closeLease())
			}
		}()
	}
	if err != nil {
		return err
	}
	if lease == nil {
		return ErrInvalid
	}
	if err := lease.Check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request, _ := handoff.request()
	tx, err := w.Journal.Admit(request)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, tx.Close()) }()
	admissionGroup, err := tx.workerResource(ResourceKindObserver, "admission-proof")
	if err != nil {
		return tx.Complete(errors.Join(err, closeLease()))
	}
	releaseLease := func() error { return tx.settleWorkerResource(admissionGroup, closeLease()) }
	if err := lease.Check(); err != nil {
		return tx.Complete(errors.Join(err, releaseLease()))
	}
	if err := ctx.Err(); err != nil {
		return tx.Complete(errors.Join(err, releaseLease()))
	}
	if err := releaseLease(); err != nil {
		return tx.Complete(errors.Join(release.ErrCleanupUncertain, err))
	}
	queryGroup, err := tx.workerResource(ResourceKindRuntime, "query-endpoint")
	if err != nil {
		return tx.Complete(err)
	}
	listener, err := w.Listen()
	if err != nil || listener == nil {
		if err == nil {
			err = ErrInvalid
		}
		var closeErr error
		if listener != nil {
			closeErr = listener.Close()
		}
		return tx.Complete(errors.Join(err, tx.settleWorkerResource(queryGroup, closeErr)))
	}
	queryContext, stopQueries := context.WithCancel(context.Background())
	queryDone := make(chan error, 1)
	go func() { queryDone <- ServeQueries(queryContext, listener, w.Journal) }()
	settleQueries := func() error { stopQueries(); return tx.settleWorkerResource(queryGroup, <-queryDone) }
	ackErr := json.NewEncoder(ack).Encode(Acceptance{Accepted: true, OperationID: request.ID, IntentSHA256: request.IntentSHA256, PID: os.Getpid()})
	// Delivery loss does not cancel accepted execution. Its teardown joins before
	// the durable terminal result, independently of acknowledgement delivery.
	return errors.Join(tx.runWithSettlement(handoff.Command, w.Store, w.Control, settleQueries), ackErr)
}
