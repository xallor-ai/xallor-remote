package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type runtimeControl struct {
	foreground *foregroundRuntime
	// Private dependency for cleanup fault tests; production always uses Prepare.
	prepareImage func(*exec.Cmd, string) (io.Closer, error)
}

func (runtimeControl) Running() (*buildinfo.Executable, error) {
	raw, err := ipc.Dial(300 * time.Millisecond)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, nil
		}
		return nil, err
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(2 * time.Second))
	return runningOnConn(ipc.NewConn(raw))
}

func runningOnConn(c *ipc.Conn) (*buildinfo.Executable, error) {
	if err := ipc.Call(c, "release-status", "status", map[string]any{}); err != nil {
		return nil, err
	}
	f, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if f.ID != "release-status" || f.Event != "" || f.OK == nil || !*f.OK {
		return nil, fmt.Errorf("无法查询运行版本。")
	}
	var result struct {
		Build buildinfo.Executable `json:"build"`
	}
	if err := json.Unmarshal(f.Result, &result); err != nil {
		return nil, err
	}
	if result.Build.Path == "" || result.Build.SHA256 == "" || result.Build.PID <= 0 {
		return nil, fmt.Errorf("当前版本无法提供完整运行身份，请检查安装版本与迁移方案。")
	}
	return &result.Build, nil
}
func (c runtimeControl) Start(path, sha string) (result error) {
	cmd := exec.Command(path, guardedArgs("runtime-serve")...)
	// Foreground means the head waits and displays output; the Runtime still
	// has an independent process group and must not create a console window.
	configureDetached(cmd)
	prepare := c.prepareImage
	if prepare == nil {
		prepare = func(cmd *exec.Cmd, sha string) (io.Closer, error) { return runtimeimage.Prepare(cmd, sha) }
	}
	image, err := prepare(cmd, sha)
	if err != nil {
		return runtimeImageAdmissionError(err)
	}
	started := false
	defer func() {
		if !started {
			if err := image.Close(); err != nil {
				result = errors.Join(result, fmt.Errorf("%w: unstarted image release: %v", release.ErrCleanupUncertain, err))
			}
		}
	}()
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	permit, err := s.NewLaunchPermit(path, sha, os.Getpid())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(permit)
	if err != nil || len(payload) > 1024 {
		return errors.New("invalid Runtime launch permit")
	}
	input, output, err := os.Pipe()
	if err != nil {
		return err
	}
	defer input.Close()
	defer output.Close()
	cmd.Stdin = input
	var log *os.File
	if c.foreground != nil {
		cmd.Stdout, cmd.Stderr = c.foreground.stdout, c.foreground.stderr
	} else {
		log, err = os.OpenFile(filepath.Join(s.Dir, "startup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		cmd.Stdout, cmd.Stderr = log, log
	}
	err = cmd.Start()
	_ = input.Close()
	if log != nil {
		_ = log.Close()
	}
	if err != nil {
		return err
	}
	started = true
	done := make(chan runtimeStartResult, 1)
	go func() {
		waitErr := cmd.Wait()
		done <- runtimeStartResult{waitErr: waitErr, imageErr: image.Close(), exited: cmd.ProcessState != nil}
	}()
	// The bounded permit fits one pipe buffer and is never put in argv/env or
	// persisted for retry. Only this child receives the read end.
	_, writeErr := output.Write(payload)
	closeErr := output.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr, stopOwnedRuntime(cmd.Process.Kill, done, 5*time.Second))
	}
	deadline := time.Now().Add(75 * time.Second) // includes child's bounded provenance admission
	for time.Now().Before(deadline) {
		select {
		case result := <-done:
			if err := result.cleanupError(); err != nil {
				return err
			}
			return fmt.Errorf("Runtime 启动后退出：%v", result.waitErr)
		default:
		}
		got, err := c.Running()
		if err == nil && got != nil {
			if got.SHA256 == sha && got.PID == cmd.Process.Pid {
				if c.foreground != nil {
					c.foreground.done = done
				}
				return nil
			}
			return errors.Join(fmt.Errorf("运行版本与待安装版本不一致。"), stopOwnedRuntime(cmd.Process.Kill, done, 5*time.Second))
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.Join(fmt.Errorf("Runtime 启动验证超时。"), stopOwnedRuntime(cmd.Process.Kill, done, 5*time.Second))
}

func runtimeImageAdmissionError(err error) error {
	if errors.Is(err, runtimeimage.ErrCleanupUncertain) {
		return errors.Join(release.ErrCleanupUncertain, err)
	}
	return err
}
