package handoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func beneath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func (m *Manager) preflight(ctx context.Context, r Request, root string) (Config, string, string, error) {
	// Official Windows CLI startup can exceed eight seconds under memory pressure.
	// Bound the whole preflight while retaining any shorter request deadline.
	probe, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	phase := "workspace"
	fail := func(code string) (Config, string, string, error) {
		if probe.Err() == context.DeadlineExceeded {
			code = "preflight_timeout_" + phase
		}
		if probe.Err() == context.Canceled {
			code = "preflight_interrupted"
		}
		return Config{}, "", "", fmt.Errorf("%s", code)
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fail("workspace_unavailable")
	}
	workspace, err := filepath.EvalSymlinks(r.Workspace)
	if err != nil || !beneath(base, workspace) {
		return fail("workspace_outside_root")
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		return m.output(probe, cmd)
	}
	phase = "git_root"
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || !samePath(strings.TrimSpace(top), workspace) {
		return fail("workspace_not_repository_root")
	}
	phase = "git_revision"
	revision, err := git("rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(revision) != r.Revision {
		return fail("revision_mismatch")
	}
	phase = "git_status"
	dirty, err := git("status", "--porcelain")
	if err != nil || strings.TrimSpace(dirty) != "" {
		return fail("workspace_dirty")
	}
	phase = "attachments"
	for _, a := range r.Attachments {
		file, err := filepath.EvalSymlinks(filepath.Join(workspace, a.Path))
		if err != nil || !beneath(workspace, file) {
			return fail("attachment_outside_workspace")
		}
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return fail("attachment_unavailable")
		}
		f, err := os.Open(file)
		if err != nil {
			return fail("attachment_unavailable")
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, 1024*1024+1))
		_ = f.Close()
		if err != nil || n > 1024*1024 || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.SHA256) {
			return fail("attachment_mismatch")
		}
	}
	phase = "adapter_config"
	var cfg Config
	if readJSON(filepath.Join(m.dir, "handoff", "adapter.json"), 16*1024, &cfg) != nil || cfg.validate() != nil {
		return fail("adapter_not_configured")
	}
	call := func(arg string) (string, error) {
		return m.output(probe, exec.Command(cfg.Executable, append(append([]string{}, cfg.Prefix...), arg)...))
	}
	phase = "cli_help"
	help, err := call("--help")
	if err != nil {
		return fail("adapter_unavailable")
	}
	for _, flag := range []string{"--resume", "--workspace", "--mode", "--print", "--output-format", "stream-json"} {
		if !strings.Contains(help, flag) {
			return fail("incompatible_cli")
		}
	}
	phase = "cli_version"
	version, err := call("--version")
	version = strings.TrimSpace(version)
	if err != nil || version == "" || len(version) > 128 || strings.ContainsAny(version, "\r\n") {
		return fail("incompatible_cli")
	}
	return cfg, version, workspace, nil
}
