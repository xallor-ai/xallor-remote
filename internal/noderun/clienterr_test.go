package noderun

import (
	"fmt"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// 目的：hello_client 失败时把协议码交给 IPC，不要一律说成中转断开。
// 前置：错误字符串是 unauthorized。预期：返回 unauthorized。
func TestShould_keepUnauthorized_whenClientHelloDenied(t *testing.T) {
	if got := clientDialCode(fmt.Errorf("%s", protocol.Unauthorized)); got != protocol.Unauthorized {
		t.Fatalf("got %q", got)
	}
}

// 目的：拨号/读失败仍归为 relay_error。
func TestShould_useRelayError_whenClientDialFails(t *testing.T) {
	if got := clientDialCode(fmt.Errorf("dial tcp")); got != protocol.RelayError {
		t.Fatalf("got %q", got)
	}
}
