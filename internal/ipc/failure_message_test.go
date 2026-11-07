package ipc

import "testing"

// Heads must show an unconfirmed mutation as such, while peers that provide
// only a code still receive the ordinary localized explanation.
func TestFailureMessage_should_preserveSpecificOutcome_andExplainBareCode(t *testing.T) {
	want := "设置已保存，尚未确认生效。"
	if got := FailureMessage(Fail("settings", "relay_error", want)); got != want {
		t.Fatalf("lost mutation outcome: %s", got)
	}
	if got := FailureMessage(Fail("settings", "relay_error", "")); got != Human("relay_error") {
		t.Fatalf("missing code explanation: %s", got)
	}
}
