//go:build windows

package process

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"time"
)

// commandIO owns adapter-created files only; caller-provided files stay open.
// Generic streams become pipes before native process creation, so their type
// never selects a weaker process-supervision path.
type commandIO struct {
	files         [3]*os.File
	child, parent []*os.File
	copies        []func() error
	done          chan error
}

func prepareCommandIO(cmd *exec.Cmd) (_ *commandIO, err error) {
	s := &commandIO{}
	defer func() {
		if err != nil {
			s.closeAll()
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
			s.child, s.parent = append(s.child, r), append(s.parent, w)
			s.copies = append(s.copies, func() error { defer w.Close(); _, e := io.Copy(w, cmd.Stdin); return e })
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
		s.child = append(s.child, f)
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
	s.child, s.parent = append(s.child, w), append(s.parent, r)
	s.copies = append(s.copies, func() error { defer r.Close(); _, e := io.Copy(writer, r); return e })
	return w, nil
}

func (s *commandIO) startCopies() {
	// The child inherited duplicates; close our writer ends so EOF is observable.
	for _, f := range s.child {
		_ = f.Close()
	}
	s.child = nil
	s.done = make(chan error, len(s.copies))
	for _, copyStream := range s.copies {
		go func(copyStream func() error) { s.done <- copyStream() }(copyStream)
	}
}

func (s *commandIO) closeAll() {
	for _, files := range [][]*os.File{s.child, s.parent} {
		for _, f := range files {
			_ = f.Close()
		}
	}
	s.child, s.parent = nil, nil
}

func (s *commandIO) finish(delay time.Duration) error {
	defer s.closeAll()
	if s.done == nil || len(s.copies) == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var errs []error
	for range s.copies {
		select {
		case err := <-s.done:
			errs = append(errs, err)
		case <-timer.C:
			// Like os/exec, arbitrary caller readers/writers must themselves
			// unblock; closing the pipe cannot interrupt user code in Write.
			s.closeAll()
			errs = append(errs, exec.ErrWaitDelay, <-s.done)
		}
	}
	return errors.Join(errs...)
}
