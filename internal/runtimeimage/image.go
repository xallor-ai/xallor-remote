// Package runtimeimage binds Runtime launch admission to an expected digest.
// Windows retains a handle denying writes/replacement. Linux executes a sealed
// memfd via its inherited descriptor. Other platforms have digest admission
// only and still require an execution-protection mechanism before acceptance.
package runtimeimage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
)

const MaxBytes = 256 * 1024 * 1024

var ErrCleanupUncertain = errors.New("Runtime image cleanup is unconfirmed")

func joinCleanup(cause, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	return errors.Join(cause, ErrCleanupUncertain, cleanup)
}

// Prepare checks cmd.Path against the caller's authenticated digest before any
// execution. Close the returned handle after Wait, or after a failed Start.
func Prepare(cmd *exec.Cmd, expectedSHA string) (*os.File, error) {
	f, err := Open(cmd.Path, expectedSHA)
	if err != nil {
		return nil, err
	}
	if err := inheritImage(cmd, f); err != nil {
		return nil, joinCleanup(err, f.Close())
	}
	return f, nil
}

// Open checks the expected digest through the same handle that the caller must
// retain until the launched process has exited. It does not authenticate source.
func Open(path, expectedSHA string) (*os.File, error) {
	return openWith(path, expectedSHA, openImage)
}

func openWith(path, expectedSHA string, open func(string) (*os.File, error)) (*os.File, error) {
	if b, err := hex.DecodeString(expectedSHA); err != nil || len(b) != sha256.Size {
		return nil, errors.New("invalid expected Runtime digest")
	}
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) {
		return nil, joinCleanup(err, f.Close())
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxBytes {
		return fail(errors.New("Runtime image must be a bounded regular file"))
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return fail(err)
	}
	if n != info.Size() || hex.EncodeToString(digest.Sum(nil)) != expectedSHA {
		return fail(errors.New("Runtime image differs from expected digest"))
	}
	return f, nil
}
