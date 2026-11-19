package runtimeprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
)

var imageDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// WatchImage retains kernel process identity and independently verifies its
// executable bytes. It authenticates neither a release nor a process's behavior.
// Callers serialize Close/Wait/Exited as for WatchExit.
func WatchImage(pid int, selectedPath, expectedSHA string) (result ExitWatch, resultErr error) {
	if !imageDigest.MatchString(expectedSHA) {
		return nil, errors.New("invalid expected process image digest")
	}
	w, err := WatchExit(pid)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, w.Close())
		}
	}()
	if exited, err := w.Exited(); err != nil || exited {
		return nil, errors.Join(ErrProcessGone, err)
	}
	f, err := openProcessImage(pid, selectedPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, f.Close())
		}
	}()
	const maximumImageBytes = 512 << 20
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumImageBytes {
		return nil, errors.New("invalid process executable file")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maximumImageBytes+1))
	if err != nil {
		return nil, err
	}
	if n > maximumImageBytes || hex.EncodeToString(h.Sum(nil)) != expectedSHA {
		return nil, errors.New("actual process image digest differs")
	}
	// A PID lookup that raced with exit/reuse cannot transfer this observer to
	// the replacement process, even when the replacement runs identical bytes.
	if exited, err := w.Exited(); err != nil || exited {
		return nil, errors.Join(ErrProcessGone, err)
	}
	keep = true
	return &imageWatch{ExitWatch: w, file: f}, nil
}

type imageWatch struct {
	ExitWatch
	file *os.File
}

func (w *imageWatch) Close() error {
	err := w.ExitWatch.Close()
	if w.file != nil {
		err = errors.Join(err, w.file.Close())
		w.file = nil
	}
	return err
}
