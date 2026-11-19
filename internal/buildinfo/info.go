// Package buildinfo identifies the artifact that is actually executing.
package buildinfo

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

//go:embed version.txt
var sourceVersion string

// Set by the release builder. Source builds use Go's VCS metadata.
var Revision string
var BuiltAt string
var Modified string

const Protocol = 1

// RuntimeOwnerProtocol versions local lifecycle ownership independently of
// the Relay/data protocol and on-disk profile format.
const RuntimeOwnerProtocol = 1

type Info struct {
	Version       string `json:"version"`
	Revision      string `json:"revision"`
	BuiltAt       string `json:"built_at,omitempty"`
	Dirty         bool   `json:"dirty"`
	Protocol      int    `json:"protocol"`
	ProfileFormat int    `json:"profile_format"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

func Version() string { return strings.TrimSpace(sourceVersion) }
func Current() Info {
	info := Info{Version: Version(), Revision: Revision, BuiltAt: BuiltAt, Dirty: Modified == "true", Protocol: Protocol, ProfileFormat: ProfileFormat(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if b, ok := debug.ReadBuildInfo(); ok {
		for _, s := range b.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Revision == "" {
					info.Revision = s.Value
				}
			case "vcs.time":
				if info.BuiltAt == "" {
					info.BuiltAt = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
	}
	if info.Revision == "" {
		info.Revision = "unknown"
	}
	return info
}

type Executable struct {
	ActivationID string `json:"activation_id,omitempty"`
	LaunchID     string `json:"launch_id,omitempty"`
	Info
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	PID           int    `json:"pid"`
	OwnerProtocol int    `json:"runtime_owner_protocol"`
}

var runningOnce sync.Once
var running Executable

// Running caches the loaded image identity before an on-disk package is upgraded.
func Running() Executable {
	runningOnce.Do(func() {
		running = Executable{Info: Current(), PID: os.Getpid(), OwnerProtocol: RuntimeOwnerProtocol}
		running.Path, _ = os.Executable()
		imagePath := running.Path
		if runtime.GOOS == "linux" {
			// A sealed memfd (or an unlinked executable) has no reopenable
			// display pathname. Hash the loaded kernel image, not a disk alias.
			imagePath = "/proc/self/exe"
		}
		running.SHA256, _ = HashFile(imagePath)
	})
	return running
}
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
