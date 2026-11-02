//go:build !windows && !linux

package process

import (
	"errors"
	"os/exec"
)

func prepare(*exec.Cmd) (boundary, error) {
	return nil, errors.New("process supervision requires Windows or Linux")
}
