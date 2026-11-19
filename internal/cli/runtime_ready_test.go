package cli

import (
	"context"
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The listener is a real kernel peer and its image is the real test process.
// Synthetic manifest/ready fields test identity rejection, not signed provenance.
func TestNativeReady_shouldBindKernelPeerImageAndManifest(t *testing.T) {
	for _, fault := range []string{"none", "peer", "launch", "image", "manifest"} {
		t.Run(fault, func(t *testing.T) {
			t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
			dir := t.TempDir()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			live := buildinfo.Running()
			live.Info.Version = "1.2.3"
			live.Info.Revision = strings.Repeat("a", 40)
			live.Info.Dirty = false
			binary := self
			info, err := os.Stat(binary)
			if err != nil {
				t.Fatal(err)
			}
			name := "xallor-remote"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			m := release.Manifest{Schema: 1, Build: live.Info, Artifact: release.Artifact{Filename: name, SHA256: live.SHA256, Size: info.Size()}}
			state := release.State{Current: &m, CurrentActivation: strings.Repeat("1", 32), CurrentLaunchID: strings.Repeat("2", 32)}
			raw, _ := json.Marshal(state)
			if err = os.WriteFile(filepath.Join(dir, "state.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(m)
			if fault == "manifest" {
				raw = []byte(`{"unknown":true}`)
			}
			manifestPath := filepath.Join(filepath.Dir(self), "manifest.json")
			file, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(manifestPath)
			if _, err = file.Write(raw); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			r := release.ReadyRequest{StoreDir: dir, Binary: binary, Manifest: m, Selection: release.ObserveSelection(state), Observer: release.ResourceObligation{ID: strings.Repeat("3", 32)}}
			if fault == "image" {
				r.Manifest.Artifact.SHA256 = strings.Repeat("f", 64)
			}
			ln, err := ipc.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				c := ipc.NewConn(conn)
				request, err := c.ReadRequest()
				if err != nil {
					return
				}
				observation := release.RuntimeObservation{State: "ready", Version: m.Build.Version, Revision: m.Build.Revision, Protocol: m.Build.Protocol, ProfileFormat: m.Build.ProfileFormat, SHA256: m.Artifact.SHA256, PID: os.Getpid(), ActivationID: state.CurrentActivation, LaunchID: state.CurrentLaunchID}
				if fault == "peer" {
					observation.PID++
				}
				if fault == "launch" {
					observation.LaunchID = strings.Repeat("f", 32)
				}
				_ = c.WriteFrame(ipc.OK(request.ID, map[string]any{"runtime": observation}))
			}()
			a, err := (runtimeControl{}).PrepareReady(r)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			observation, runErr := a.Run(ctx)
			closures, closeErr := a.Close()
			again, againErr := a.Close()
			if len(closures) != 1 || len(again) != 1 || closures[0] != again[0] || (closeErr == nil) != (againErr == nil) {
				t.Fatal("cleanup receipt not stable", closures, again, closeErr, againErr)
			}
			if fault == "none" {
				if runErr != nil || closeErr != nil || !observation.Valid(r) {
					t.Fatal(observation, runErr, closeErr)
				}
			} else if runErr == nil {
				t.Fatal("native mismatch accepted", fault, observation)
			}
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Fatal("native ready server leaked")
			}
		})
	}
}
