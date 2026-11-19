package cli

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

type transactionAdmission struct {
	ctx                context.Context
	store              release.Store
	parent             runtimeprocess.ExitWatch
	connection         net.Conn
	owner              buildinfo.Executable
	parentPID, peerPID int
	permit             release.OwnerHandoff
	authorized         bool
	closeOnce          sync.Once
	closeErr           error
}

// Before reading inherited input, require the OS parent to be the serving IPC
// process and to execute the same image as this worker. Full provenance and
// operation/profile binding are still mandatory after decoding the envelope.
func openTransactionParent(ctx context.Context, s release.Store, worker buildinfo.Executable, parentPID int) (*transactionAdmission, error) {
	if parentPID <= 0 || parentPID == worker.PID {
		return nil, errors.New("invalid lifecycle parent")
	}
	raw, err := ipc.Dial(time.Second)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			raw.Close()
		}
	}()
	peer, err := ipc.PeerProcessID(raw)
	if err != nil || peer != parentPID {
		return nil, errors.New("lifecycle parent is not the IPC server")
	}
	_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
	owner, err := runningOnConn(ipc.NewConn(raw))
	if err != nil {
		return nil, err
	}
	if owner == nil || owner.PID != parentPID || owner.SHA256 != worker.SHA256 || owner.Info != worker.Info || owner.OwnerProtocol != worker.OwnerProtocol {
		return nil, errors.New("lifecycle parent and worker identities differ")
	}
	parent, err := runtimeprocess.WatchImage(parentPID, worker.Path, worker.SHA256)
	if err != nil {
		return nil, err
	}
	keep = true
	return &transactionAdmission{ctx: ctx, store: s, parent: parent, connection: raw, owner: *owner, parentPID: parentPID, peerPID: peer}, nil
}

func (a *transactionAdmission) Check() error {
	if !a.authorized {
		return errors.New("lifecycle handoff is not authenticated")
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if exited, err := a.parent.Exited(); err != nil || exited {
		return errors.Join(errors.New("lifecycle parent no longer available"), err)
	}
	return a.store.CheckOwnerHandoffState(a.permit)
}

func (a *transactionAdmission) Close() error {
	a.closeOnce.Do(func() { a.closeErr = errors.Join(a.connection.Close(), a.parent.Close()) })
	return a.closeErr
}
