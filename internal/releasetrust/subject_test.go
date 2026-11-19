package releasetrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The private fake gh is a native process that only returns controlled public
// proof JSON. This exercises verifier wiring, not cryptographic authenticity.
func TestMain(m *testing.M) {
	if output, ok := os.LookupEnv("XR_TEST_GH_VERIFIED_SUBJECT"); ok {
		if output == "__oversized__" {
			fmt.Print(strings.Repeat(" ", maxVerificationOutput+1))
			hash := sha256.Sum256([]byte("retained candidate"))
			output = fmt.Sprintf(`[{"verificationResult":{"statement":{"subject":[{"digest":{"sha256":"%x"}}]}}}]`, hash)
		}
		fmt.Print(output)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Exit zero for a signed sibling must not authenticate the retained candidate.
// Real release statements include multiple subjects sharing a basename.
func TestVerify_should_bindSuccessfulVerifierOutput_toExpectedSubject(t *testing.T) {
	tools := t.TempDir()
	source := t.TempDir()
	artifact := filepath.Join(source, "artifact")
	data := []byte("retained candidate")
	if err := os.WriteFile(artifact, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(tools, name), image, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	for _, tc := range []struct {
		name, output string
		accepted     bool
	}{
		{"other_subject", `[{"verificationResult":{"statement":{"subject":[{"digest":{"sha256":"` + strings.Repeat("b", 64) + `"}}]}}}]`, false},
		{"empty", "[]", false},
		{"invalid", "not JSON", false},
		{"oversized", "__oversized__", false},
		{"raw_envelope_only", `[{"attestation":{"subject":[{"digest":{"sha256":"` + expected + `"}}]}}]`, false},
		{"matching_among_subjects", `[{"verificationResult":{"statement":{"subject":[{"name":"manifest.json","digest":{"sha256":"other"}},{"name":"manifest.json","digest":{"sha256":"` + expected + `"}}]}}}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XR_TEST_GH_VERIFIED_SUBJECT", tc.output)
			err := Verify(context.Background(), artifact, Identity{Version: "0.2.6", Revision: strings.Repeat("a", 40)}, "", source, expected)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v expected_digest=%s", tc.accepted, err, expected)
			}
		})
	}
}
