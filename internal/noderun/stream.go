package noderun

import "github.com/xallor-ai/xallor-remote/internal/protocol"

// takeChunk clips one stdout/stderr frame to remaining quota and max frame size.
// drain is true when the caller should keep reading but stop sending.
func takeChunk(sent int, chunk []byte) (send []byte, newSent int, trunc, drain bool) {
	if len(chunk) == 0 {
		return nil, sent, false, false
	}
	if len(chunk) > protocol.MaxFrameBytes {
		chunk = chunk[:protocol.MaxFrameBytes]
		trunc = true
	}
	remain := protocol.MaxExecOutputBytes - sent
	if remain <= 0 {
		return nil, sent, true, true
	}
	if len(chunk) > remain {
		chunk = chunk[:remain]
		return chunk, sent + len(chunk), true, true
	}
	return chunk, sent + len(chunk), trunc, false
}
