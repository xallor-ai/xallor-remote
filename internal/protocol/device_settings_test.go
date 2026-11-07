package protocol

import (
	"strings"
	"testing"
)

// Settings require one bounded correlation key and a complete snapshot. A hash
// is SHA-256 lowercase hexadecimal; an enabled device cannot omit its grant.
func TestDeviceSettings_should_rejectPartialOrMalformedSnapshot(t *testing.T) {
	valid := Message{RequestID: "settings", GrantHash: SHA256Hex("grant"), Inbound: Bool(true)}
	if !valid.ValidDeviceSettings() {
		t.Fatal("valid snapshot rejected")
	}
	for _, change := range []func(*Message){
		func(m *Message) { m.RequestID = "" },
		func(m *Message) { m.RequestID = strings.Repeat("a", 129) },
		func(m *Message) { m.Inbound = nil },
		func(m *Message) { m.GrantHash = "" },
		func(m *Message) { m.GrantHash = strings.Repeat("x", 64) },
		func(m *Message) { m.GrantHash = strings.ToUpper(m.GrantHash) },
	} {
		m := valid
		change(&m)
		if m.ValidDeviceSettings() {
			t.Fatal("malformed snapshot accepted")
		}
	}
	if !(Message{RequestID: "disable", Inbound: Bool(false)}).ValidDeviceSettings() {
		t.Fatal("empty disabled snapshot rejected")
	}
}
