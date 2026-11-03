package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type runtimeControl struct{}

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
	c := ipc.NewConn(raw)
	if err = ipc.Call(c, "release-status", "status", map[string]any{}); err != nil {
		return nil, err
	}
	f, err := c.ReadFrame()
	if err != nil {
		return nil, err
	}
	if f.OK == nil || !*f.OK {
		return nil, fmt.Errorf("无法查询运行版本。")
	}
	var result struct {
		Build buildinfo.Executable `json:"build"`
	}
	if err = json.Unmarshal(f.Result, &result); err != nil {
		return nil, err
	}
	if result.Build.Path == "" || result.Build.SHA256 == "" {
		return nil, fmt.Errorf("当前版本不支持验证升级。请先停止旧服务，再安装；身份与配置会保留。")
	}
	return &result.Build, nil
}
func (c runtimeControl) Stop() error {
	live, err := c.Running()
	if err != nil {
		return err
	}
	if live == nil {
		return nil
	}
	if _, err = rpc("stop", map[string]any{}); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		next, err := c.Running()
		if err == nil && next == nil {
			return nil
		}
		if next != nil && next.PID != live.PID {
			return fmt.Errorf("另一进程已启动，停止操作已中止。")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("停止超时。")
}
func (runtimeControl) Check(path string) (buildinfo.Executable, error) {
	var info buildinfo.Executable
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(b, &info)
	return info, err
}
func (c runtimeControl) Start(path, sha string) error {
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(s.Dir, "startup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, guardedArgs("start")...)
	cmd.Stdout = log
	cmd.Stderr = log
	err = detach(cmd)
	log.Close()
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return fmt.Errorf("Runtime 启动后退出：%v", err)
		default:
		}
		got, err := c.Running()
		if err == nil && got != nil {
			if got.SHA256 == sha && got.PID == cmd.Process.Pid {
				return nil
			}
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("运行版本与待安装版本不一致。")
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	<-done
	return fmt.Errorf("Runtime 启动验证超时。")
}
