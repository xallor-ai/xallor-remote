package runtimehead

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

func lifecycleNamespace() (string, error) {
	profile, err := appdata.DataDir()
	if err != nil {
		return "", err
	}
	return (runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}).NamespaceID()
}

// QueryLifecycle tries the transaction endpoint then the current serving owner.
// It sends only the read-only query; no endpoint error can trigger bootstrap.
func QueryLifecycle(ctx context.Context, id string) (runtimeowner.Record, error) {
	var errs []error
	for _, dial := range []func(time.Duration) (net.Conn, error){ipc.DialLifecycle, ipc.Dial} {
		if err := ctx.Err(); err != nil {
			return runtimeowner.Record{}, err
		}
		raw, err := dial(300 * time.Millisecond)
		if err == nil {
			var record runtimeowner.Record
			record, err = queryLifecycleOn(ctx, raw, id)
			_ = raw.Close()
			if err == nil {
				return record, nil
			}
		}
		errs = append(errs, err)
	}
	return runtimeowner.Record{}, errors.Join(errs...)
}

func queryLifecycleOn(ctx context.Context, raw net.Conn, id string) (runtimeowner.Record, error) {
	namespace, err := lifecycleNamespace()
	if err != nil {
		return runtimeowner.Record{}, err
	}
	peerPID, err := ipc.PeerProcessID(raw)
	if err != nil {
		return runtimeowner.Record{}, err
	}
	parent, err := runtimeprocess.WatchExit(peerPID)
	if err != nil {
		return runtimeowner.Record{}, err
	}
	defer parent.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return runtimeowner.Record{}, err
	}
	c := ipc.NewConn(raw)
	const requestID = "lifecycle-query"
	if err := ipc.Call(c, requestID, "runtime.operation.status", map[string]string{"operation_id": id}); err != nil {
		return runtimeowner.Record{}, err
	}
	frame, err := c.ReadFrame()
	if err != nil {
		return runtimeowner.Record{}, err
	}
	if frame.ID != requestID || frame.Event != "" || frame.OK == nil {
		return runtimeowner.Record{}, runtimeowner.ErrInvalid
	}
	if !*frame.OK {
		if frame.Code == protocol.RuntimeOperationUnknown {
			return runtimeowner.Record{}, runtimeowner.ErrUnknown
		}
		return runtimeowner.Record{}, &Refusal{Code: frame.Code, Message: ipc.FailureMessage(frame)}
	}
	var response struct {
		Operation runtimeowner.Record  `json:"operation"`
		Settled   bool                 `json:"settled"`
		Observer  buildinfo.Executable `json:"observer"`
	}
	if frame.Code != "" || json.Unmarshal(frame.Result, &response) != nil {
		return runtimeowner.Record{}, runtimeowner.ErrInvalid
	}
	if err := response.Operation.ValidateID(id); err != nil {
		return runtimeowner.Record{}, err
	}
	if err := response.Operation.ValidateNamespace(namespace); err != nil {
		return runtimeowner.Record{}, err
	}
	profile, err := appdata.DataDir()
	if err != nil {
		return runtimeowner.Record{}, err
	}
	proofContext, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := (release.Store{Dir: filepath.Join(profile, "runtime")}).AuthenticateLifecycleObserver(proofContext, response.Observer, peerPID); err != nil {
		return runtimeowner.Record{}, err
	}
	if exited, err := parent.Exited(); err != nil || exited {
		return runtimeowner.Record{}, errors.Join(runtimeowner.ErrUncertain, err)
	}
	if response.Operation.Outcome != runtimeowner.Pending && response.Operation.Outcome != runtimeowner.Uncertain && !response.Settled {
		return runtimeowner.Record{}, runtimeowner.ErrPending
	}
	return response.Operation, nil
}
