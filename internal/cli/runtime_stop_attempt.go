package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type runtimeStopAttempt struct {
	observer     release.ResourceObligation
	intent       release.StopIntent
	raw          net.Conn
	conn         *ipc.Conn
	watch, image runtimeprocess.ExitWatch
	used, closed bool
	closeErr     error
	resources    func() (string, string, error)
	before       release.SelectionEvidence
	store        release.Store
	managed      bool
}

func (c runtimeControl) PrepareStop(ctx context.Context, request release.StopRequest) (release.StopAttempt, error) {
	return prepareRuntimeStop(ctx, request, request.ObserveResources)
}

func prepareRuntimeStop(ctx context.Context, request release.StopRequest, resources func() (string, string, error)) (attempt release.StopAttempt, result error) {
	attemptID := request.Observer.AttemptID
	if attemptID == "" {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		attemptID = hex.EncodeToString(nonce[:])
	}
	a := &runtimeStopAttempt{observer: request.Observer, intent: release.StopIntent{AttemptID: attemptID}, resources: resources}
	dir, err := appdata.DataDir()
	if err != nil {
		return a, err
	}
	a.managed = request.AbsencePolicy != "public_observation"
	a.before = request.Before
	a.store = release.Store{Dir: filepath.Join(dir, "runtime")}
	if a.resources == nil {
		a.resources = func() (string, string, error) { return observeStoppedResources(dir) }
	}
	if err := ctx.Err(); err != nil {
		return a, err
	}
	raw, err := ipc.Dial(300 * time.Millisecond)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return a, err
		}
		if request.AbsencePolicy == "" || (request.AbsencePolicy == "explicit_legacy_recovery" && resources == nil) {
			return a, errors.Join(release.ErrCleanupUncertain, errors.New("Runtime absent without admitted quiescence policy"))
		}
		a.intent.Mode = "no_peer"
		a.intent.AbsenceBasis = request.AbsencePolicy
		return a, nil
	}
	a.raw = raw
	a.conn = ipc.NewConn(raw)
	_ = raw.SetDeadline(time.Now().Add(2 * time.Second))
	pid, err := ipc.PeerProcessID(raw)
	if err != nil || pid <= 0 {
		return a, errors.Join(release.ErrCleanupUncertain, err)
	}
	a.watch, err = runtimeprocess.WatchExit(pid)
	if err != nil {
		return a, errors.Join(release.ErrCleanupUncertain, err)
	}
	live, err := runningOnConn(a.conn)
	if err != nil {
		return a, err
	}
	if live.PID != pid {
		return a, errors.Join(release.ErrCleanupUncertain, errors.New("Runtime peer PID differs from status"))
	}
	a.intent.Mode = "peer"
	a.intent.PeerPID = pid
	a.intent.ImageSHA256 = live.SHA256
	a.intent.IdentityScope = "process_image"
	path := live.Path
	if request.AbsencePolicy != "public_observation" {
		id := release.SelectionIdentity{SHA256: live.SHA256, ActivationID: live.ActivationID, LaunchID: live.LaunchID}
		allowed := false
		for _, item := range request.Allowed {
			allowed = allowed || item == id
		}
		if !id.Complete() || !allowed {
			return a, errors.Join(release.ErrCleanupUncertain, errors.New("Runtime authenticated launch differs from allowed stop targets"))
		}
		s := a.store
		st, err := s.Read()
		if err != nil {
			return a, errors.Join(release.ErrCleanupUncertain, err)
		}
		if release.ObserveSelection(st) != request.Before {
			return a, errors.Join(release.ErrCleanupUncertain, errors.New("stop selection changed"))
		}
		// Activation resolves the canonical snapshot path, including native suffixes.
		m := st.Current
		if st.PendingActivation == id.ActivationID {
			m = st.Pending
		}
		if m == nil {
			return a, release.ErrCleanupUncertain
		}
		activation, err := s.Activation(id.ActivationID, *m)
		if err != nil {
			return a, err
		}
		path = activation.Binary
		a.intent.Selection = id
		a.intent.IdentityScope = "process_image_and_launch"
	}
	a.image, err = runtimeprocess.WatchImage(pid, path, live.SHA256)
	if err != nil {
		// Capture can fail after opening hidden members and join their Close
		// failures. A nil image does not prove those members were released.
		a.closeErr = errors.Join(a.closeErr, err)
		return a, errors.Join(release.ErrCleanupUncertain, err)
	}
	if exited, err := a.watch.Exited(); err != nil || exited {
		return a, errors.Join(release.ErrCleanupUncertain, runtimeprocess.ErrProcessGone, err)
	}
	if err := ctx.Err(); err != nil {
		return a, err
	}
	return a, nil
}

func (a *runtimeStopAttempt) Intent() release.StopIntent { return a.intent }
func (a *runtimeStopAttempt) Close() ([]release.StopClosure, error) {
	if a.closed {
		return []release.StopClosure{{Resource: a.observer, Closed: a.closeErr == nil}}, a.closeErr
	}
	a.closed = true
	if a.image != nil {
		a.closeErr = errors.Join(a.closeErr, a.image.Close())
	}
	if a.watch != nil {
		a.closeErr = errors.Join(a.closeErr, a.watch.Close())
	}
	if a.raw != nil {
		a.closeErr = errors.Join(a.closeErr, a.raw.Close())
	}
	return []release.StopClosure{{Resource: a.observer, Closed: a.closeErr == nil}}, a.closeErr
}
func (a *runtimeStopAttempt) Run(parent context.Context) (o release.StopObservation, result error) {
	o = release.StopObservation{AttemptID: a.intent.AttemptID, Dispatch: "not_sent", Process: "not_observed", RuntimeResource: "unknown", Jobs: "unreleased_or_unknown", Observers: "unknown", Result: "unknown"}
	if a.used || a.closed {
		return o, release.ErrCleanupUncertain
	}
	a.used = true
	defer func() {
		if _, err := a.Close(); err != nil {
			result = errors.Join(result, release.ErrCleanupUncertain, err)
		} else {
			o.Observers = "released"
		}
		if result == nil {
			if a.intent.Mode == "peer" {
				o.Result = "released_peer"
			} else {
				o.Result = "quiescent_no_peer"
			}
		}
	}()
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	if a.managed {
		st, err := a.store.Read()
		if err != nil || release.ObserveSelection(st) != a.before {
			return o, errors.Join(release.ErrCleanupUncertain, errors.New("prepared stop selection changed"), err)
		}
	}
	if err := ctx.Err(); err != nil {
		return o, errors.Join(release.ErrCleanupUncertain, err)
	}
	if a.intent.Mode == "peer" {
		o.Process = "still_running_or_unknown"
		exited, err := a.watch.Exited()
		if err != nil {
			return o, errors.Join(release.ErrCleanupUncertain, err)
		}
		if !exited {
			deadline, _ := ctx.Deadline()
			_ = a.raw.SetDeadline(deadline)
			o.Dispatch = "may_have_been_sent"
			if err := ipc.Call(a.conn, "release-stop", "stop", map[string]any{}); err != nil {
				result = errors.Join(release.ErrCleanupUncertain, err)
			} else {
				frame, err := a.conn.ReadFrame()
				if err != nil || frame.ID != "release-stop" || frame.Event != "" || frame.OK == nil || !*frame.OK {
					result = errors.Join(release.ErrCleanupUncertain, fmt.Errorf("stop acknowledgement missing: %v", err))
				} else {
					o.Dispatch = "acknowledged"
				}
			}
		}
		if err := a.watch.Wait(ctx); err != nil {
			return o, errors.Join(result, stopCleanupError(err))
		}
		o.Process = "exit_observed"
	}
	var err error
	o.RuntimeResource, o.Jobs, err = a.resources()
	if err != nil {
		return o, errors.Join(result, stopCleanupError(err))
	}
	return o, result
}

func observeStoppedResources(dir string) (string, string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "absent_profile", "absent", nil
	}
	unlock, err := filelock.Acquire(filepath.Join(dir, "runtime.lock"))
	if err != nil {
		return "unknown", "unreleased_or_unknown", err
	}
	defer unlock()
	jobs, err := job.ObserveReleased(dir)
	return "released_under_lock", jobs, jobCleanupError(err)
}
