// package-supervisor runs the installer under existing Runtime process supervision.
// It is a test tool, not a new product execution backend or security sandbox.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

type observation struct {
	Started          bool   `json:"started"`
	PID              int    `json:"pid"`
	ExitCode         int    `json:"exit_code"`
	CleanupConfirmed bool   `json:"cleanup_confirmed"`
	TimedOut         bool   `json:"timed_out"`
	Boundary         string `json:"boundary"`
}

func supervise(ctx context.Context, args []string) (result observation, resultErr error) {
	result.ExitCode = -1
	switch runtime.GOOS {
	case "windows":
		result.Boundary = "windows_job"
	case "linux":
		result.Boundary = "linux_process_group"
	default:
		return result, errors.New("native Windows/Linux process supervision required")
	}
	if len(args) == 0 || !filepath.IsAbs(args[0]) {
		return result, errors.New("absolute trusted executable required")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	process.HideConsole(cmd)
	child, err := process.Start(ctx, cmd)
	if err != nil {
		return result, err
	}
	result.Started, result.PID = true, cmd.Process.Pid
	err = child.Wait()
	result.TimedOut = ctx.Err() != nil
	result.CleanupConfirmed = !errors.Is(err, process.ErrCleanup) && !errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result, errors.Join(err, ctx.Err())
}

func main() {
	timeout := flag.Duration("timeout", 10*time.Minute, "installer deadline including supervised cancellation")
	flag.Parse()
	if *timeout <= 0 || *timeout > 15*time.Minute {
		fmt.Fprintln(os.Stderr, "invalid supervision deadline")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := supervise(ctx, flag.Args())
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !result.Started || !result.CleanupConfirmed || result.TimedOut || result.ExitCode != 0 {
		os.Exit(1)
	}
}
