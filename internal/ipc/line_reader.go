package ipc

import (
	"bufio"
	"bytes"
	"errors"
	"io"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var ErrMessageTooLarge = errors.New("IPC 报文超过上限")

type lineReader struct{ scanner *bufio.Scanner }

func newLineReader(r io.Reader) *lineReader {
	s := bufio.NewScanner(r)
	// Include room for the delimiter and one byte proving an oversized record.
	s.Buffer(make([]byte, 4096), protocol.MaxIPCMessageBytes+2)
	s.Split(splitIPCLine)
	return &lineReader{scanner: s}
}

func splitIPCLine(data []byte, atEOF bool) (int, []byte, error) {
	if end := bytes.IndexByte(data, '\n'); end >= 0 {
		if end > protocol.MaxIPCMessageBytes {
			return 0, nil, ErrMessageTooLarge
		}
		return end + 1, data[:end], nil
	}
	if len(data) > protocol.MaxIPCMessageBytes {
		return 0, nil, ErrMessageTooLarge
	}
	if atEOF && len(data) > 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return 0, nil, nil
}

func (r *lineReader) read() ([]byte, error) {
	if r.scanner.Scan() {
		return r.scanner.Bytes(), nil
	}
	if err := r.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}
