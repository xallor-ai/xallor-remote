package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// A native endpoint reporting another process must be refused before stop;
// neither an already exited PID nor a helper exiting on acknowledgement proves
// that the still-live IPC server has stopped.
func TestStop_should_refuseMismatchedPeer_beforeDispatch(t *testing.T) {
	for _, mode := range []string{"missing-process", "exited-process", "live-helper", "wrong-image", "wrong-launch", "selection-read"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
			identity := buildinfo.Running()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var cmd *exec.Cmd
			if mode == "wrong-image" {
				identity.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			} else if mode == "wrong-launch" || mode == "selection-read" {
				identity.ActivationID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				identity.LaunchID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			} else if mode == "missing-process" {
				identity.PID = 2147483647
				watch, err := runtimeprocess.WatchExit(identity.PID)
				if watch != nil {
					_ = watch.Close()
				}
				if !errors.Is(err, runtimeprocess.ErrProcessGone) {
					t.Fatalf("missing PID precondition failed: %v", err)
				}
			} else {
				cmd = exec.CommandContext(ctx, self, "version", "--json")
				configureDetached(cmd)
				probeMode := "normal"
				if mode == "live-helper" {
					probeMode = "hang"
				}
				cmd.Env = append(os.Environ(), "XR_TEST_VERSION_PROBE="+probeMode)
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				}()
				if mode == "exited-process" {
					if err := cmd.Wait(); err != nil {
						t.Fatal(err)
					}
				}
				identity.PID = cmd.Process.Pid
			}
			ln, err := ipc.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			type observation struct {
				stop bool
				err  error
			}
			observed := make(chan observation, 1)
			go func() {
				raw, err := ln.Accept()
				if err != nil {
					observed <- observation{err: err}
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
				conn := ipc.NewConn(raw)
				req, err := conn.ReadRequest()
				if err != nil {
					observed <- observation{err: err}
					return
				}
				if err := conn.WriteFrame(ipc.OK(req.ID, map[string]any{"build": identity})); err != nil {
					observed <- observation{err: err}
					return
				}
				req, err = conn.ReadRequest()
				if err != nil {
					observed <- observation{}
					return
				}
				if req.Method == "stop" {
					_ = conn.WriteFrame(ipc.OK(req.ID, map[string]any{}))
					if mode == "live-helper" {
						_ = cmd.Process.Kill()
					}
					observed <- observation{stop: true}
					return
				}
				observed <- observation{err: errors.New("unexpected method after status")}
			}()
			if mode == "wrong-launch" || mode == "selection-read" {
				allowed := release.SelectionIdentity{SHA256: identity.SHA256, ActivationID: identity.ActivationID, LaunchID: "cccccccccccccccccccccccccccccccc"}
				if mode == "selection-read" {
					allowed.LaunchID = identity.LaunchID
					store, e := runtimeStore()
					if e != nil {
						t.Fatal(e)
					}
					if e = os.MkdirAll(store.Dir, 0700); e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(filepath.Join(store.Dir, "state.json"), []byte("invalid"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				group := release.ResourceObligation{ID: "dddddddddddddddddddddddddddddddd", AttemptID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Generation: 1, Phase: "effects", Kind: "observer", ObserverScope: "owner_process", Retention: "release_required"}
				attempt, captureErr := (runtimeControl{}).PrepareStop(ctx, release.StopRequest{Observer: group, Before: release.ObserveSelection(release.State{}), Allowed: []release.SelectionIdentity{allowed}})
				if attempt == nil {
					t.Fatal("partial native capture lost its owned handles", captureErr)
				}
				native := attempt.(*runtimeStopAttempt)
				if native.raw == nil || native.watch == nil || native.closed || attempt.Intent().AttemptID != group.AttemptID {
					t.Fatal("partial capture missing original ownership", native)
				}
				for n := 0; n < 2; n++ {
					closures, closeErr := attempt.Close()
					if e := release.StopCleanup(group, closures, closeErr); e != nil {
						t.Fatal("partial native cleanup unproven", e)
					}
				}

				err = captureErr
			} else if mode == "wrong-image" {
				attempt, captureErr := (runtimeControl{}).PrepareStop(ctx, release.StopRequest{AbsencePolicy: "public_observation"})
				if attempt == nil || captureErr == nil {
					t.Fatal("missing partial image capture", attempt, captureErr)
				}
				// WatchImage's error can contain an internal Close failure; its nil
				// observer cannot certify release of the hidden native members.
				closures, closeErr := attempt.Close()
				if closeErr == nil || len(closures) != 1 || closures[0].Closed {
					t.Fatal("failed image capture incorrectly certified cleanup", closures, closeErr)
				}
				err = captureErr
			} else {
				err = (runtimeControl{}).Stop()
			}
			if !errors.Is(err, release.ErrCleanupUncertain) {
				t.Errorf("expected uncertain cleanup for mismatched native peer, got %v", err)
			}
			select {
			case got := <-observed:
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.stop {
					t.Error("stop was dispatched to a peer reporting another process")
				}
			case <-ctx.Done():
				t.Fatal("native peer fixture did not finish")
			}
		})
	}
}
