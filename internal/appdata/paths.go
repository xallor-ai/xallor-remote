package appdata

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DataDir() (string, error) {
	if dir := os.Getenv("XALLOR_REMOTE_DATA_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("XALLOR_REMOTE_DATA_DIR must be absolute")
		}
		return filepath.Clean(dir), nil
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "XallorRemote"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "xallor-remote"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "xallor-remote"), nil
}

func DefaultWorkspace() (string, error) {
	if os.Getenv("XALLOR_REMOTE_DATA_DIR") != "" {
		dir, err := DataDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "workspace"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "XallorRemote", "workspace"), nil
	}
	return filepath.Join(home, "XallorRemote", "workspace"), nil
}

func IPCPath() (string, error) {
	if os.Getenv("XALLOR_REMOTE_DATA_DIR") != "" {
		dir, err := DataDir()
		if err != nil {
			return "", err
		}
		if runtime.GOOS == "windows" {
			sum := sha256.Sum256([]byte(strings.ToLower(dir)))
			return fmt.Sprintf(`\\.\pipe\XallorRemote-%x`, sum[:8]), nil
		}
		return filepath.Join(dir, "ipc.sock"), nil
	}
	if runtime.GOOS == "windows" {
		return `\\.\pipe\XallorRemote`, nil
	}
	if rtd := os.Getenv("XDG_RUNTIME_DIR"); rtd != "" {
		return filepath.Join(rtd, "xallor-remote.sock"), nil
	}
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ipc.sock"), nil
}

func DefaultRelayURL() string {
	if u := os.Getenv("XALLOR_REMOTE_RELAY_URL"); u != "" {
		return u
	}
	return "wss://api.xallor.com/remote"
}
