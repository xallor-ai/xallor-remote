//go:build windows

package process

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"
)

// commandIO owns adapter-created files only; caller-provided files stay open.
// Generic streams become pipes before native process creation, so their type
// never selects a weaker process-supervision path.
type commandIO struct {
	files         [3]*os.File
	child, parent []*ownedIOFile
	copies        []func() error
	done          chan error
}

func prepareCommandIO(cmd *exec.Cmd) (_ *commandIO, err error) {
	s := &commandIO{}
	defer func() {
		if err != nil {
			err = failedStartCleanup(err, s.closeAll())
		}
	}()
	if cmd.Stdin == nil {
		s.files[0], err = s.null(os.O_RDONLY)
	} else if f, ok := cmd.Stdin.(*os.File); ok {
		s.files[0] = f
	} else {
		var r, w *os.File
		r, w, err = os.Pipe()
		if err == nil {
			s.files[0] = r
			s.child, s.parent = append(s.child, ownIO(r)), append(s.parent, ownIO(w))
			owned := s.parent[len(s.parent)-1]
			s.copies = append(s.copies, func() error { _, e := io.Copy(w, cmd.Stdin); return failedStartCleanup(e, owned.Close()) })
		}
	}
	if err != nil {
		return nil, err
	}
	if s.files[1], err = s.output(cmd.Stdout); err != nil {
		return nil, err
	}
	// A shared comparable writer uses one pipe/copy loop, avoiding concurrent
	// writes to destinations such as bytes.Buffer, just as os/exec does.
	if cmd.Stdout != nil && reflect.TypeOf(cmd.Stdout).Comparable() && cmd.Stdout == cmd.Stderr {
		s.files[2] = s.files[1]
	} else if s.files[2], err = s.output(cmd.Stderr); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *commandIO) null(flags int) (*os.File, error) {
	f, err := os.OpenFile(os.DevNull, flags, 0)
	if err == nil {
		s.child = append(s.child, ownIO(f))
	}
	return f, err
}

func (s *commandIO) output(writer io.Writer) (*os.File, error) {
	if writer == nil {
		return s.null(os.O_WRONLY)
	}
	if f, ok := writer.(*os.File); ok {
		return f, nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	s.child, s.parent = append(s.child, ownIO(w)), append(s.parent, ownIO(r))
	owned := s.parent[len(s.parent)-1]
	s.copies = append(s.copies, func() error { _, e := io.Copy(writer, r); return failedStartCleanup(e, owned.Close()) })
	return w, nil
}

// An adapter-owned file is closed exactly once, even when the copy goroutine
// races a forced drain shutdown. Preserve the first close failure on all paths.
type ownedIOFile struct {
	file *os.File
	once sync.Once
	err  error
}

func ownIO(f *os.File) *ownedIOFile { return &ownedIOFile{file: f} }
func (f *ownedIOFile) Close() error { f.once.Do(func() { f.err = f.file.Close() }); return f.err }
func (s *commandIO) startCopies() error {
	var errs []error
	for _, f := range s.child {
		errs = append(errs, f.Close())
	}
	if err := errors.Join(errs...); err != nil {
		return failedStartCleanup(nil, err)
	}
	s.done = make(chan error, len(s.copies))
	for _, copyStream := range s.copies {
		go func(copyStream func() error) { s.done <- copyStream() }(copyStream)
	}
	return nil
}
func (s *commandIO) closeAll() error {
	var errs []error
	for _, files := range [][]*ownedIOFile{s.child, s.parent} {
		for _, f := range files {
			errs = append(errs, f.Close())
		}
	}
	return errors.Join(errs...)
}
func (s *commandIO) finish(delay time.Duration) (err error) {
	defer func() { err = failedStartCleanup(err, s.closeAll()) }()
	if s.done == nil || len(s.copies) == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var errs []error
	for range s.copies {
		select {
		case e := <-s.done:
			errs = append(errs, e)
		case <-timer.C:
			// Closing owned pipes unblocks native IO. Arbitrary caller code in Write
			// must still return; probe capture writers are bounded and nonblocking.
			errs = append(errs, failedStartCleanup(nil, s.closeAll()), exec.ErrWaitDelay, <-s.done)
		}
	}
	return errors.Join(errs...)
}
