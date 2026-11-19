package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
	"net"
	"time"
)

type runtimeReadyAttempt struct {
	request        release.ReadyRequest
	raw            net.Conn
	image, process runtimeprocess.ExitWatch
	used, closed   bool
	closeErr       error
}

func (runtimeControl) PrepareReady(r release.ReadyRequest) (release.ReadyAttempt, error) {
	return &runtimeReadyAttempt{request: r}, nil
}

func (a *runtimeReadyAttempt) Run(ctx context.Context) (release.ReadyObservation, error) {
	var result release.ReadyObservation
	if a.used || a.closed {
		return result, errors.New("ready attempt already consumed")
	}
	a.used = true
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var err error
	a.raw, err = ipc.Dial(300 * time.Millisecond)
	if err != nil {
		return result, err
	}
	deadline, _ := ctx.Deadline()
	if err = a.raw.SetDeadline(deadline); err != nil {
		return result, err
	}
	pid, err := ipc.PeerProcessID(a.raw)
	if err != nil || pid <= 0 {
		return result, errors.Join(release.ErrCleanupUncertain, err)
	}
	a.process, err = runtimeprocess.WatchExit(pid)
	if err != nil {
		return result, err
	}
	a.image, err = runtimeprocess.WatchImage(pid, a.request.Binary, a.request.Manifest.Artifact.SHA256)
	if err != nil {
		a.closeErr = errors.Join(a.closeErr, err)
		return result, err
	}
	c := ipc.NewConn(a.raw)
	if err = ipc.Call(c, "activation-ready", "runtime.ensure", map[string]any{"required_profile_format": buildinfo.ProfileFormat(), "required_protocol": buildinfo.Protocol}); err != nil {
		return result, err
	}
	f, err := c.ReadFrame()
	if err != nil {
		return result, err
	}
	var reply struct {
		Runtime release.RuntimeObservation `json:"runtime"`
	}
	if f.ID != "activation-ready" || f.Event != "" || f.OK == nil || !*f.OK || f.Code != "" || json.Unmarshal(f.Result, &reply) != nil {
		return result, errors.New("invalid ready response")
	}
	live := reply.Runtime
	target := a.request.Selection.Current
	if live.ValidateReady(buildinfo.ProfileFormat(), buildinfo.Protocol) != nil || live.PID != pid || live.ActivationID != target.ActivationID || live.LaunchID != target.LaunchID || live.SHA256 != target.SHA256 || live.Version != a.request.Manifest.Build.Version || live.Revision != a.request.Manifest.Build.Revision {
		return result, errors.New("ready process identity mismatch")
	}
	manifestSHA, sourceSHA, err := release.CaptureReadyManifest(a.request.Binary, a.request.Manifest)
	if err != nil {
		if errors.Is(err, release.ErrCleanupUncertain) {
			a.closeErr = errors.Join(a.closeErr, err)
		}
		return result, err
	}
	selected, err := (release.Store{Dir: a.request.StoreDir}).Read()
	if err != nil {
		return result, err
	}
	if release.ObserveSelection(selected) != a.request.Selection {
		return result, errors.New("ready selection changed")
	}
	for _, watch := range []runtimeprocess.ExitWatch{a.image, a.process} {
		exited, err := watch.Exited()
		if err != nil || exited {
			return result, errors.Join(release.ErrCleanupUncertain, err)
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	return release.ReadyObservation{Selection: a.request.Selection, PeerPID: int64(pid), ImageSHA256: live.SHA256, ManifestSHA256: manifestSHA, SourceSnapshotSHA256: sourceSHA, Authenticated: true}, nil
}

func (a *runtimeReadyAttempt) Close() ([]release.ReadyClosure, error) {
	if a.closed {
		return []release.ReadyClosure{{Resource: a.request.Observer, Closed: a.closeErr == nil}}, a.closeErr
	}
	a.closed = true
	if a.image != nil {
		a.closeErr = errors.Join(a.closeErr, a.image.Close())
	}
	if a.process != nil {
		a.closeErr = errors.Join(a.closeErr, a.process.Close())
	}
	if a.raw != nil {
		a.closeErr = errors.Join(a.closeErr, a.raw.Close())
	}
	return []release.ReadyClosure{{Resource: a.request.Observer, Closed: a.closeErr == nil}}, a.closeErr
}
