package runtimehead

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func successfulOperation(command runtimeowner.Command) runtimeowner.Record {
	r, _ := command.Request()
	sha := strings.Repeat("b", 64)
	namespace, err := lifecycleNamespace()
	if err != nil {
		panic(err)
	}
	target := release.SelectionIdentity{SHA256: sha, ActivationID: strings.Repeat("1", 32), LaunchID: strings.Repeat("2", 32)}
	empty := release.ObserveSelection(release.State{})
	pending := release.SelectionEvidence{StateSHA256: strings.Repeat("c", 64), Pending: target}
	current := release.SelectionEvidence{StateSHA256: strings.Repeat("d", 64), Current: target}
	manifest := release.Manifest{Schema: 1, Build: buildinfo.Info{Version: "0.2.2", Revision: strings.Repeat("a", 40), Protocol: buildinfo.Protocol, ProfileFormat: buildinfo.ProfileFormat(), OS: runtime.GOOS, Arch: runtime.GOARCH}, Artifact: release.Artifact{Filename: func() string {
		if runtime.GOOS == "windows" {
			return "xallor-remote.exe"
		}
		return "xallor-remote"
	}(), SHA256: sha, Size: 1}}
	rawManifest, _ := json.Marshal(manifest)
	manifestSum := sha256.Sum256(rawManifest)
	binding := release.ResolvedIntent{Baseline: empty, Requested: release.SelectionIdentity{SHA256: sha, ActivationID: target.ActivationID}, Manifest: manifest, ManifestSHA256: hex.EncodeToString(manifestSum[:])}
	prepareExpected := pending
	stopIntent := release.StopIntent{Mode: "peer", AttemptID: strings.Repeat("3", 32), PeerPID: 123, ImageSHA256: sha, Selection: target, IdentityScope: "process_image_and_launch"}
	return runtimeowner.Record{Request: r, NamespaceSHA256: namespace, Binding: &binding, Outcome: runtimeowner.Succeeded, Effects: []runtimeowner.Effect{
		{EffectIntent: release.EffectIntent{Name: "prepare", Purpose: "requested", TargetSHA256: sha, Target: target, Before: empty, Expected: &prepareExpected}, Observed: &pending, State: runtimeowner.EffectConfirmed},
		{EffectIntent: release.EffectIntent{Name: "stop", Purpose: "requested", Before: pending, Stop: &stopIntent}, StopObserved: &release.StopObservation{AttemptID: stopIntent.AttemptID, Dispatch: "acknowledged", Process: "exit_observed", RuntimeResource: "released_under_lock", Jobs: "empty", Observers: "released", Result: "released_peer"}, Observed: &pending, State: runtimeowner.EffectConfirmed},
		{EffectIntent: release.EffectIntent{Name: "start", Purpose: "requested", TargetSHA256: sha, Target: target, Before: pending}, Observed: &pending, State: runtimeowner.EffectConfirmed},
		{EffectIntent: release.EffectIntent{Name: "commit", Purpose: "requested", TargetSHA256: sha, Target: target, Before: pending, Expected: &current}, Observed: &current, State: runtimeowner.EffectConfirmed},
		{EffectIntent: release.EffectIntent{Name: "ready", Purpose: "requested", TargetSHA256: sha, Target: target, Before: current}, Observed: &current, State: runtimeowner.EffectConfirmed}}}
}

// An authenticated owner can definitively refuse before launching a worker.
// That refusal must not turn into minutes of polling an operation never admitted.
func TestLifecycleDefiniteRefusalDoesNotPollMissingReceipt(t *testing.T) {
	command, _ := runtimeowner.NewCommand("rollback", "")
	a, b := net.Pipe()
	defer b.Close()
	go func() {
		defer a.Close()
		c := ipc.NewConn(a)
		req, err := c.ReadRequest()
		if err == nil {
			c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeSourceUnverified, "来源未验证"))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queries := 0
	_, err := SubmitLifecycle(ctx, b, ipc.NewConn(b), command, func(context.Context, string) (runtimeowner.Record, error) {
		queries++
		return runtimeowner.Record{}, runtimeowner.ErrUnknown
	})
	if err == nil || queries != 0 {
		t.Fatal("definite refusal polled missing receipt", err, queries)
	}
}

// Losing an acceptance reply must only query the original ID. The test server
// closes immediately after receiving the mutation; the client never resends it.
func TestLifecycleLostAcceptanceQueriesWithoutMutationReplay(t *testing.T) {
	command, err := runtimeowner.NewCommand("install", filepath.Join(t.TempDir(), "bundle"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer b.Close()
	var mutations, queries atomic.Int32
	go func() {
		defer a.Close()
		c := ipc.NewConn(a)
		req, err := c.ReadRequest()
		if err == nil && req.Method == "runtime.install" {
			mutations.Add(1)
		}
	}()
	query := func(context.Context, string) (runtimeowner.Record, error) {
		queries.Add(1)
		r := successfulOperation(command)
		return r, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := SubmitLifecycle(ctx, b, ipc.NewConn(b), command, query); err != nil {
		t.Fatal(err)
	}
	if mutations.Load() != 1 || queries.Load() != 1 {
		t.Fatal(mutations.Load(), queries.Load())
	}
}

// The query callback may observe a changed ambient profile after dispatch.
// Result correlation must retain the namespace used for the original request.
func TestLifecycle_should_retainOriginalNamespace_acrossFollowupQueries(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	original, err := lifecycleNamespace()
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	command, _ := runtimeowner.NewCommand("rollback", "")
	a, b := net.Pipe()
	defer b.Close()
	done := make(chan string, 1)
	go func() {
		defer a.Close()
		c := ipc.NewConn(a)
		req, err := c.ReadRequest()
		if err != nil {
			done <- ""
			return
		}
		var params map[string]string
		_ = json.Unmarshal(req.Params, &params)
		done <- params["namespace_sha256"]
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"accepted": true, "operation_id": command.ID}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = SubmitLifecycle(ctx, b, ipc.NewConn(b), command, func(context.Context, string) (runtimeowner.Record, error) {
		t.Setenv("XALLOR_REMOTE_DATA_DIR", other)
		return successfulOperation(command), nil
	})
	if err == nil {
		t.Fatal("profile change rebound an accepted operation")
	}
	if got := <-done; got != original {
		t.Fatal("request was not bound before dispatch", got)
	}
}

// A success envelope containing another intent or incomplete effect evidence
// cannot certify the request. Rejected/uncertain outcomes never become success.
func TestLifecycleRejectsUnboundOrNonSuccessfulReceipt(t *testing.T) {
	for _, kind := range []string{"intent", "namespace", "incomplete", "rejected", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			command, _ := runtimeowner.NewCommand("rollback", "")
			a, b := net.Pipe()
			defer b.Close()
			go func() {
				defer a.Close()
				c := ipc.NewConn(a)
				req, err := c.ReadRequest()
				if err == nil {
					c.WriteFrame(ipc.OK(req.ID, map[string]any{"accepted": true, "operation_id": command.ID}))
				}
			}()
			query := func(context.Context, string) (runtimeowner.Record, error) {
				r := successfulOperation(command)
				switch kind {
				case "namespace":
					r.NamespaceSHA256 = strings.Repeat("f", 64)
				case "intent":
					r.Request.IntentSHA256 = strings.Repeat("c", 64)
				case "incomplete":
					r.Effects = nil
				case "rejected":
					r.Outcome = runtimeowner.Rejected
					r.Effects = nil
				case "uncertain":
					r.Outcome = runtimeowner.Uncertain
				}
				return r, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := SubmitLifecycle(ctx, b, ipc.NewConn(b), command, query); err == nil {
				t.Fatal("invalid/non-successful receipt accepted")
			}
		})
	}
}

// Querying must use only operation.status, and malformed envelopes/IDs refuse.
func TestQueryLifecycleValidatesCorrelatedEnvelope(t *testing.T) {
	command, _ := runtimeowner.NewCommand("rollback", "")
	a, b := net.Pipe()
	defer b.Close()
	go func() {
		defer a.Close()
		c := ipc.NewConn(a)
		req, err := c.ReadRequest()
		if err != nil {
			return
		}
		if req.Method != "runtime.operation.status" {
			return
		}
		c.WriteFrame(ipc.OK("wrong-id", map[string]any{"operation": successfulOperation(command)}))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := queryLifecycleOn(ctx, b, command.ID); err == nil {
		t.Fatal("uncorrelated result accepted")
	}
}
