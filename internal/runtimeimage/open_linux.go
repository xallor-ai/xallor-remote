//go:build linux

package runtimeimage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const requiredImageSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

func openImage(path string) (result *os.File, resultErr error) {
	source, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			resultErr = joinCleanup(resultErr, closeErr)
			if result != nil {
				resultErr = joinCleanup(resultErr, result.Close())
				result = nil
			}
		}
	}()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxBytes {
		return nil, errors.New("Runtime image must be a bounded regular file")
	}
	name, err := linuxImageName(path)
	if err != nil {
		return nil, err
	}
	flags := unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	fd, err := unix.MemfdCreate(name, flags|unix.MFD_EXEC)
	// Before Linux 6.3 executable memfds have no MFD_EXEC flag. EINVAL for
	// this fixed-size name/flag set means the optional flag is unsupported.
	// Permission/policy errors never fall back, and no unsealed image executes.
	if errors.Is(err, unix.EINVAL) {
		fd, err = unix.MemfdCreate(name, flags)
	}
	if err != nil {
		return nil, err
	}
	image := os.NewFile(uintptr(fd), name)
	fail := func(err error) (*os.File, error) { return nil, joinCleanup(err, image.Close()) }
	n, err := io.Copy(image, io.LimitReader(source, MaxBytes+1))
	if err != nil {
		return fail(err)
	}
	if n != info.Size() {
		return fail(errors.New("Runtime source size changed during image capture"))
	}
	if err := image.Chmod(0500); err != nil {
		return fail(err)
	}
	_, err = unix.FcntlInt(image.Fd(), unix.F_ADD_SEALS, requiredImageSeals|unix.F_SEAL_EXEC)
	if errors.Is(err, unix.EINVAL) {
		_, err = unix.FcntlInt(image.Fd(), unix.F_ADD_SEALS, requiredImageSeals)
	}
	if err != nil {
		return fail(err)
	}
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&requiredImageSeals != requiredImageSeals {
		return fail(errors.New("Runtime execution image is not sealed"))
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	// Open verifies the digest only after sealing, against these immutable bytes.
	return image, nil
}

func inheritImage(cmd *exec.Cmd, image *os.File) error {
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&requiredImageSeals != requiredImageSeals {
		return errors.New("cannot execute an unsealed Runtime image")
	}
	fd := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, image)
	cmd.Path = fmt.Sprintf("/proc/self/fd/%d", fd)
	return nil
}

// The kernel name binds an observation to its selected activation pathname.
// It is a diagnostic label, never provenance or proof of byte identity by itself.
func linuxImageName(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(absolute))
	return "xr-runtime-" + hex.EncodeToString(digest[:]), nil
}

func sealedPathMatches(observed, selected string) bool {
	name, err := linuxImageName(selected)
	// os.Executable trims the kernel's " (deleted)" display suffix.
	return err == nil && observed == "/memfd:"+name
}
