package protocol

import "testing"

func TestShould_newDeviceID_beNineDigits(t *testing.T) {
	id, err := NewDeviceID()
	if err != nil {
		t.Fatal(err)
	}
	if !IsNumericDeviceID(id) {
		t.Fatalf("id=%q", id)
	}
}

func TestShould_rejectNonNumericDeviceID(t *testing.T) {
	for _, id := range []string{"", "andy", "dev_aabbcc", "12345678", "1234567890"} {
		if IsNumericDeviceID(id) {
			t.Fatalf("accepted %q", id)
		}
	}
}
