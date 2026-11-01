package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaxWSMessageBytes_should_fit_worst_case_write_when_content_is_1mib_controls(t *testing.T) {
	content := strings.Repeat("\x01", MaxWriteBytes)
	path := strings.Repeat("p", MaxPathBytes)
	raw, err := json.Marshal(Message{
		Type:   TypeInvoke,
		ExecID: "ex_" + strings.Repeat("a", 32),
		Op:     OpWrite,
		Payload: mustJSON(WritePayload{
			Path:    path,
			Content: content,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > MaxWSMessageBytes {
		t.Fatalf("invoke wire %d > MaxWSMessageBytes %d", len(raw), MaxWSMessageBytes)
	}
}

func TestMaxWSMessageBytes_should_fit_common_1mib_text_vectors(t *testing.T) {
	for _, ch := range []string{"x", `\`, `"`, "中"} {
		raw, err := json.Marshal(Message{
			Type: TypeInvoke, ExecID: "ex_test", Op: OpWrite,
			Payload: mustJSON(WritePayload{Path: "a.txt", Content: strings.Repeat(ch, MaxWriteBytes/len(ch))}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > MaxWSMessageBytes {
			t.Fatalf("%q wire %d > %d", ch, len(raw), MaxWSMessageBytes)
		}
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
