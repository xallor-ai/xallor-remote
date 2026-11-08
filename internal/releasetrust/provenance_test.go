package releasetrust

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Candidate metadata cannot set the repository, signer, root, host or predicate.
// This checks policy arguments only; it does not claim cryptographic acceptance.
func TestPolicy_should_pinTrustedIdentity_whenConstructingVerifierArguments(t *testing.T) {
	i := Identity{Version: "0.2.1", Revision: strings.Repeat("a", 40)}
	args, err := Arguments("artifact", i, "proof.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][]string{{"--hostname", "github.com"}, {"--repo", Repository}, {"--signer-workflow", Workflow},
		{"--cert-oidc-issuer", Issuer}, {"--predicate-type", Predicate}, {"--source-ref", "refs/tags/v0.2.1"}, {"--source-digest", i.Revision}, {"--bundle", "proof.json"}} {
		found := false
		for n := 0; n+1 < len(args); n++ {
			if reflect.DeepEqual(args[n:n+2], pair) {
				found = true
			}
		}
		if !found {
			t.Fatal("missing fixed policy", pair, args)
		}
	}
	for _, arg := range args {
		if arg == "--custom-trusted-root" || arg == "--no-public-good" {
			t.Fatal("unsafe override", args)
		}
	}
}

// Missing revisions, dirty builds and branch-shaped version input fail locally.
func TestPolicy_should_rejectUnsignedDevelopmentIdentity_withoutRunningTool(t *testing.T) {
	for _, identity := range []Identity{{Version: "0.2.1", Revision: strings.Repeat("a", 40), Dirty: true},
		{Version: "0.2.1", Revision: "unknown"}, {Version: "refs/heads/main", Revision: strings.Repeat("a", 40)}} {
		if err := identity.Validate(); !errors.Is(err, ErrUnverified) {
			t.Fatal(identity, err)
		}
	}
}

// A verifier executable in the supplied artifact tree is not an independent root.
func TestVerify_should_refuseToolInsideCandidateDirectory_beforeExecution(t *testing.T) {
	dir := t.TempDir()
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("must-not-execute"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	err := Verify(context.Background(), filepath.Join(dir, "artifact"), Identity{Version: "0.2.1", Revision: strings.Repeat("a", 40)}, "", dir)
	if !errors.Is(err, ErrUnverified) || !strings.Contains(err.Error(), "待验目录内") {
		t.Fatal(err)
	}
}

// Opt-in uses the real installed gh against a malformed proof. A rejection here
// is not evidence of a valid product signature or a successful install chain.
func TestRealGH_should_refuseMalformedProof_withoutAuthenticationMock(t *testing.T) {
	if os.Getenv("XALLOR_TEST_REAL_GH") != "1" {
		t.Skip("real gh negative check is opt-in")
	}
	tool, err := exec.LookPath("gh")
	if err != nil {
		t.Fatal("real-gh check requested but GitHub CLI is missing", err)
	}
	version, err := exec.Command(tool, "--version").Output()
	if err != nil {
		t.Fatal("installed GitHub CLI cannot run", err)
	}
	t.Logf("real verifier: %s (%s)", tool, strings.SplitN(string(version), "\n", 2)[0])
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact")
	proof := filepath.Join(dir, "proof.json")
	if err := os.WriteFile(artifact, []byte("unsigned-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proof, []byte(`{"mediaType":"invalid","signature":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Verify(ctx, artifact, Identity{Version: "0.2.1", Revision: strings.Repeat("a", 40)}, proof, dir); !errors.Is(err, ErrUnverified) || !strings.Contains(err.Error(), "检查失败") {
		t.Fatal("malformed proof accepted", err)
	}
}
