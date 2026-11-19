//go:build linux

package process

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func init() {
	if os.Getenv("XR_ZOMBIE_LEADER_FIXTURE") != "1" {
		return
	}
	runtime.LockOSThread()
	go func() {
		for {
			time.Sleep(time.Second)
		}
	}()
	// SYS_EXIT terminates only the initial thread. Other Go threads remain live,
	// giving /proc a zombie leader without a signalled process pidfd.
	unix.RawSyscall(unix.SYS_EXIT, 0, 0, 0)
	os.Exit(2)
}

// A zombie leader is not complete process termination while threads survive.
// Settlement must wait for the native process signal, without reaping the root.
func TestGroup_shouldWaitForThreads_whenLeaderIsZombie(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "XR_ZOMBIE_LEADER_FIXTURE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL); _ = cmd.Wait() }()
	stat := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat"
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(stat)
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
		if fields[0] == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture leader never became zombie")
		}
		time.Sleep(time.Millisecond)
	}
	fd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if _, err = unix.Poll(fds, 0); err != nil || fds[0].Revents != 0 {
		t.Fatalf("fixture already exited: %v %v", fds, err)
	}
	settled := make(chan error, 1)
	go func() { settled <- (&group{pid: cmd.Process.Pid}).settle() }()
	select {
	case err := <-settled:
		t.Fatalf("settled before live threads exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = unix.Kill(-cmd.Process.Pid, unix.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-settled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("settlement did not complete")
	}
	if _, err = unix.Poll(fds, 0); err != nil || fds[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("settled without exit signal: %v %v", fds, err)
	}
}
