//go:build linux

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type group struct{ pid int }

func prepare(cmd *exec.Cmd) (boundary, error) {
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	attr.Setpgid, attr.Pgid = true, 0
	cmd.SysProcAttr = &attr
	return &group{}, nil
}
func (g *group) attach(cmd *exec.Cmd) error { g.pid = cmd.Process.Pid; return nil }
func (g *group) waitRoot() error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, g.pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return err
		}
	}
}
func (g *group) stop() error {
	if g.pid == 0 {
		return nil
	}
	err := unix.Kill(-g.pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

// The root remains unreaped throughout this check, reserving the process group
// ID. Zombies have ceased execution; their reaping belongs to their OS parent.
// Deliberate setsid/setpgid escapes are outside process-group supervision.
func (g *group) settle() error {
	deadline := time.Now().Add(cleanupTimeout)
	for {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return err
		}
		active := false
		for _, e := range entries {
			if _, err := strconv.Atoi(e.Name()); err != nil {
				continue
			}
			data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
			if os.IsNotExist(err) || errors.Is(err, unix.ESRCH) {
				continue
			}
			if err != nil {
				return err
			}
			end := strings.LastIndexByte(string(data), ')')
			if end < 0 {
				return errors.New("invalid process stat")
			}
			fields := strings.Fields(string(data[end+1:]))
			if len(fields) < 3 {
				return errors.New("short process stat")
			}
			pgid, err := strconv.Atoi(fields[2])
			if err != nil {
				return err
			}
			if pgid == g.pid && fields[0] != "Z" && fields[0] != "X" && fields[0] != "x" {
				active = true
				break
			}
		}
		if !active {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("process group still active")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (g *group) close() error { return nil }
