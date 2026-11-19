package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

func selectProfile(profile string) error {
	if !filepath.IsAbs(profile) {
		return errors.New("explicit absolute profile required")
	}
	info, err := os.Stat(profile)
	if err != nil || !info.IsDir() {
		return errors.New("profile directory unavailable")
	}
	return os.Setenv("XALLOR_REMOTE_DATA_DIR", filepath.Clean(profile))
}

// Only the two fixed read-only methods are accepted. The native peer reference
// spans response decoding/validation, and every descriptor closure is checked.
func queryPeer(profile string, lifecycle bool, id, method string, params any, decode func(ipc.Frame, int) error) (resultErr error) {
	if (lifecycle && method != "runtime.operation.status") || (!lifecycle && method != "runtime.status") {
		return errors.New("read-only observer method required")
	}
	if err := selectProfile(profile); err != nil {
		return err
	}
	dial, budget := ipc.Dial, 65*time.Second
	if lifecycle {
		dial, budget = ipc.DialLifecycle, 3*time.Second
	}
	raw, err := dial(time.Second)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, raw.Close()) }()
	if err := raw.SetDeadline(time.Now().Add(budget)); err != nil {
		return err
	}
	peer, err := ipc.PeerProcessID(raw)
	if err != nil {
		return err
	}
	watch, err := runtimeprocess.WatchExit(peer)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, watch.Close()) }()
	if exited, err := watch.Exited(); err != nil || exited {
		return errors.New("peer already exited")
	}
	conn := ipc.NewConn(raw)
	if err := ipc.Call(conn, id, method, params); err != nil {
		return err
	}
	frame, err := conn.ReadFrame()
	if err != nil {
		return err
	}
	if frame.ID != id || frame.OK == nil || !*frame.OK || frame.Event != "" || frame.Code != "" {
		return errors.New("observation envelope invalid")
	}
	if err := decode(frame, peer); err != nil {
		return err
	}
	if exited, err := watch.Exited(); err != nil || exited {
		return errors.New("peer exited during observation")
	}
	return nil
}
