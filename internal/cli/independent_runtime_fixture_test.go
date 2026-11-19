package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

type independentLaunch struct {
	ProfileSHA256    string `json:"profile_sha256"`
	PID              int    `json:"pid"`
	SHA256           string `json:"image_sha256"`
	Activation       string `json:"activation_id"`
	Launch           string `json:"launch_id"`
	KernelExit       bool   `json:"kernel_exit"`
	Reaped           bool   `json:"reaped"`
	EndpointAbsent   bool   `json:"endpoint_absent"`
	StartupConfirmed bool   `json:"startup_confirmed"`
	StartupUncertain bool   `json:"startup_uncertain"`
	watch            runtimeprocess.ExitWatch
	foreground       *foregroundRuntime
}

type independentProfile struct {
	dir       string
	endpoint  string
	current   *independentLaunch
	attempted bool
}

type independentHarness struct {
	t                      *testing.T
	root, bundle, relayURL string
	rootIdentity           os.FileInfo
	manifest               release.Manifest
	profiles               []*independentProfile
	launches               []*independentLaunch
	connections            []net.Conn
	cases                  []string
	cleanupErrors          []string
	observations           map[string]any
	afterStop              []func() error
}

// Test artifacts and gh are the existing native mechanical fixtures. Only
// source attestation is controlled; install, runtime-serve, IPC and exit are real.
// One short owned root also avoids Unix-domain socket path length problems.
func newIndependentHarness(t *testing.T, relayURL string) *independentHarness {
	t.Helper()
	_, binary, manifest := nativeOwnerBundle(t)
	root, err := os.MkdirTemp("", "xr-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	h := &independentHarness{t: t, root: root, bundle: filepath.Dir(binary), relayURL: relayURL, manifest: manifest, cleanupErrors: []string{}, observations: map[string]any{}}
	t.Cleanup(h.cleanup) // registered before the first launch, even partial startup
	h.rootIdentity, err = os.Lstat(root)
	if err != nil || !h.rootIdentity.IsDir() || !os.SameFile(h.rootIdentity, h.rootIdentity) {
		t.Fatal("created test root identity unavailable", err)
	}
	for _, name := range []string{"target", "client"} {
		h.profiles = append(h.profiles, &independentProfile{dir: filepath.Join(root, name)})
	}
	return h
}

// Parent env selection is serialized; running children have private inherited
// environment copies. No process-wide environment is changed by goroutines.
func (h *independentHarness) inProfile(p *independentProfile, fn func() error) (result error) {
	keys := []string{"XALLOR_REMOTE_DATA_DIR", "XALLOR_REMOTE_RELAY_URL"}
	values := []string{p.dir, h.relayURL}
	for i, key := range keys {
		old, present := os.LookupEnv(key)
		defer func(key, old string, present bool) {
			var err error
			if present {
				err = os.Setenv(key, old)
			} else {
				err = os.Unsetenv(key)
			}
			result = errors.Join(result, err)
		}(key, old, present)
		if err := os.Setenv(key, values[i]); err != nil {
			return err
		}
	}
	return fn()
}

func (h *independentHarness) start(p *independentProfile, initial bool) error {
	return h.inProfile(p, func() error {
		p.attempted = true
		fg := &foregroundRuntime{stdout: io.Discard, stderr: io.Discard}
		launch := &independentLaunch{foreground: fg}
		digest := sha256.Sum256([]byte(p.dir))
		launch.ProfileSHA256 = hex.EncodeToString(digest[:])
		h.launches = append(h.launches, launch)
		p.current = launch
		var err error
		p.endpoint, err = appdata.IPCPath()
		if err != nil {
			return err
		}
		s, err := runtimeStore()
		if err != nil {
			return err
		}
		control := runtimeControl{foreground: fg}
		if initial {
			err = s.Install(h.bundle, control)
		} else {
			err = s.EnsureRunning(h.bundle, control)
		}
		// Capture any published child even if install/readiness returned an error.
		launch.StartupUncertain = errors.Is(err, release.ErrCleanupUncertain)
		live, observeErr := control.Running()
		if live != nil {
			launch.PID, launch.SHA256, launch.Activation, launch.Launch = live.PID, live.SHA256, live.ActivationID, live.LaunchID
			launch.watch, observeErr = runtimeprocess.WatchExit(live.PID)
		}
		if err != nil || observeErr != nil {
			return errors.Join(err, observeErr)
		}
		if live == nil || live.PID == os.Getpid() || live.SHA256 != h.manifest.Artifact.SHA256 || live.OwnerProtocol != buildinfo.RuntimeOwnerProtocol || fg.done == nil {
			return errors.New("independent owner identity or foreground ownership missing")
		}
		if handled, err := ownerEnsure(); !handled || err != nil {
			return errors.Join(errors.New("independent owner not ready"), err)
		}
		launch.StartupConfirmed = true
		return nil
	})
}

func (h *independentHarness) connect(p *independentProfile) (*ipc.Conn, error) {
	return h.connectUntil(p, time.Now().Add(15*time.Second))
}

func (h *independentHarness) connectUntil(p *independentProfile, deadline time.Time) (*ipc.Conn, error) {
	var raw net.Conn
	err := h.inProfile(p, func() error {
		var err error
		raw, err = ipc.Dial(time.Second)
		if err != nil {
			return err
		}
		pid, err := ipc.PeerProcessID(raw)
		if err != nil || p.current == nil || pid != p.current.PID {
			raw.Close()
			return errors.New("IPC kernel peer differs from owned Runtime")
		}
		return raw.SetDeadline(deadline)
	})
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return nil, err
	}
	h.connections = append(h.connections, raw)
	return ipc.NewConn(raw), nil
}

func (h *independentHarness) stop(p *independentProfile) error {
	if !p.attempted {
		return nil
	}
	return h.inProfile(p, func() error {
		if err := (runtimeControl{}).Stop(); err != nil {
			return err
		}
		if current := p.current; current != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if current.watch != nil {
				if err := current.watch.Wait(ctx); err != nil {
					return err
				}
				current.KernelExit = true
				if err := current.watch.Close(); err != nil {
					return err
				}
				current.watch = nil
			} else if current.PID > 0 && !current.KernelExit {
				return errors.New("owned process exit watch missing")
			}
			if current.foreground != nil && current.foreground.done != nil && !current.Reaped {
				select {
				case result := <-current.foreground.done:
					if result.waitErr != nil || result.cleanupError() != nil {
						return errors.Join(result.waitErr, result.cleanupError())
					}
					current.Reaped = true
				case <-ctx.Done():
					return errors.New("foreground process wait not joined")
				}
			}
		}
		raw, err := ipc.Dial(100 * time.Millisecond)
		if raw != nil {
			raw.Close()
			return errors.New("stopped profile IPC still accepts connections")
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("IPC absence not proven: %w", err)
		}
		if p.current != nil {
			p.current.EndpointAbsent = true
			if p.current.StartupUncertain || !p.current.StartupConfirmed || !p.current.KernelExit || !p.current.Reaped {
				return errors.New("partial startup identity/lifetime not fully observed; retain profile")
			}
		}
		p.attempted = false
		return nil
	})
}

func (h *independentHarness) cleanup() {
	for _, conn := range h.connections {
		_ = conn.Close()
	}
	for i := len(h.profiles) - 1; i >= 0; i-- {
		if err := h.stop(h.profiles[i]); err != nil {
			h.cleanupErrors = append(h.cleanupErrors, fmt.Sprintf("profile %d: %v", i, err))
			h.t.Error("independent Runtime cleanup unconfirmed", err)
		}
	}
	for _, check := range h.afterStop {
		if err := check(); err != nil {
			h.cleanupErrors = append(h.cleanupErrors, err.Error())
			h.t.Error("task cleanup unconfirmed", err)
		}
	}
	// Exact root was created by this harness. Never remove uncertain live state.
	for _, launch := range h.launches {
		if launch.watch != nil {
			if err := launch.watch.Close(); err != nil {
				h.cleanupErrors = append(h.cleanupErrors, err.Error())
				h.t.Error(err)
			}
			launch.watch = nil
		}
	}
	removed := false
	if len(h.cleanupErrors) == 0 {
		err := removeIndependentRoot(h.root, h.rootIdentity)
		if err != nil {
			h.cleanupErrors = append(h.cleanupErrors, err.Error())
			h.t.Error(err)
		} else {
			removed = true
		}
	}
	report := map[string]any{"passed": !h.t.Failed(), "scope": "independent runtime-serve processes and real OS IPC; controlled native provenance fixture",
		"signed_release": false, "relay_transport": "loopback ws when configured; no TLS acceptance", "launches": h.launches, "cases": h.cases, "observations": h.observations, "cleanup_errors": h.cleanupErrors, "profile_root_removed": removed}
	raw, err := json.Marshal(report)
	if err != nil {
		h.t.Error(err)
		return
	}
	h.t.Log("INDEPENDENT_RUNTIME_REPORT " + string(raw))
	// Explicit caller directory supports local and container runs without source
	// tree writes, fixed CWD assumptions or stale package-local reports.
	if dir := os.Getenv("XR_INDEPENDENT_REPORT_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			h.t.Error("report directory must be absolute")
			return
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			h.t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, h.t.Name()+".json"), raw, 0600); err != nil {
			h.t.Error(err)
		}
	}
}

func independentCall(c *ipc.Conn, id, method string, params any) (ipc.Frame, error) {
	if err := ipc.Call(c, id, method, params); err != nil {
		return ipc.Frame{}, err
	}
	f, err := c.ReadFrame()
	if err == nil && (f.ID != id || f.Event != "" || f.OK == nil) {
		return ipc.Frame{}, errors.New("IPC reply correlation/shape mismatch")
	}
	return f, err
}

func requireIndependentOK(t *testing.T, f ipc.Frame, err error) {
	t.Helper()
	// Never print raw session replies or their capabilities on failure.
	if err != nil || f.OK == nil || !*f.OK {
		t.Fatalf("independent IPC failed: code=%s error=%v", f.Code, err)
	}
}
