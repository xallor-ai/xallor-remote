package runtimeowner

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// ServeQueries owns the listener and at most eight short, read-only connections.
// Shutdown closes idle readers/writers and joins them before the owner unlocks.
func ServeQueries(ctx context.Context, listener net.Listener, journal Journal) (result error) {
	var mu sync.Mutex
	active := map[*queryConnection]struct{}{}
	var closeErr error
	retainClose := func(err error) {
		if err != nil {
			mu.Lock()
			if closeErr == nil {
				closeErr = err
			}
			mu.Unlock()
		}
	}
	var listenerOnce sync.Once
	var workers sync.WaitGroup
	slots := make(chan struct{}, 8)
	closeAll := func() {
		listenerOnce.Do(func() { retainClose(listener.Close()) })
		mu.Lock()
		connections := make([]*queryConnection, 0, len(active))
		for conn := range active {
			connections = append(connections, conn)
		}
		mu.Unlock()
		for _, conn := range connections {
			retainClose(conn.Close())
		}
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); closeAll() })
	defer func() {
		if stop() {
			closeAll()
		} else {
			<-closed
		}
		workers.Wait()
		if closeErr != nil {
			result = errors.Join(result, release.ErrCleanupUncertain, closeErr)
		}
	}()
	for {
		accepted, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		conn := &queryConnection{Conn: accepted}
		select {
		case slots <- struct{}{}:
		default:
			retainClose(conn.Close())
			continue
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			retainClose(conn.Close())
			<-slots
			return nil
		}
		active[conn] = struct{}{}
		workers.Add(1)
		mu.Unlock()
		go func() {
			defer workers.Done()
			defer func() { retainClose(conn.Close()); mu.Lock(); delete(active, conn); mu.Unlock(); <-slots }()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			framed := ipc.NewConn(conn)
			request, err := framed.ReadRequest()
			if err != nil {
				return
			}
			var response ipc.Frame
			if request.Method == "runtime.operation.reconcile" {
				response = journal.ReconcileObservation(request)
			} else {
				response = journal.QueryOperation(request)
			}
			_ = framed.WriteFrame(response)
		}()
	}
}

// Both shutdown and the serving goroutine can request closure. Cache the exact
// first close result instead of issuing a second close on an already closed fd.
type queryConnection struct {
	net.Conn
	once     sync.Once
	closeErr error
}

func (c *queryConnection) Close() error {
	c.once.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}
