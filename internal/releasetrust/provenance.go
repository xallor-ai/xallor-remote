// Package releasetrust verifies provenance using an independently trusted gh.
// Candidate keys, checksums and previous success records are never trust roots.
package releasetrust

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const Repository = "xallor-ai/xallor-remote"
const Workflow = Repository + "/.github/workflows/release.yml"
const Issuer = "https://token.actions.githubusercontent.com"
const Predicate = "https://slsa.dev/provenance/v1"

var ErrUnverified = errors.New("发行来源未验证")
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Identity struct {
	Version  string
	Revision string
	Dirty    bool
}

func (i Identity) Validate() error {
	if i.Dirty || !versionPattern.MatchString(i.Version) || !revisionPattern.MatchString(i.Revision) {
		return fmt.Errorf("%w：需要正式版本号、完整 commit 和非 dirty 制品", ErrUnverified)
	}
	return nil
}

// Arguments contains only the built-in source policy; bundle is untrusted proof
// data for gh, never a custom trust root or an executable command fragment.
func Arguments(artifact string, identity Identity, bundle string) ([]string, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	args := []string{"attestation", "verify", artifact, "--hostname", "github.com", "--repo", Repository,
		"--signer-workflow", Workflow, "--cert-oidc-issuer", Issuer, "--predicate-type", Predicate,
		"--source-ref", "refs/tags/v" + identity.Version, "--source-digest", identity.Revision,
		"--deny-self-hosted-runners"}
	if bundle != "" {
		args = append(args, "--bundle", bundle)
	}
	return args, nil
}

func within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Verify fails on missing tools, unavailable services and invalid proofs. The
// caller must independently trust the system gh installation outside sourceRoot.
func Verify(ctx context.Context, artifact string, identity Identity, bundle, sourceRoot string) error {
	args, err := Arguments(artifact, identity, bundle)
	if err != nil {
		return err
	}
	tool, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("%w：请安装可信的 GitHub CLI", ErrUnverified)
	}
	tool, err = filepath.EvalSymlinks(tool)
	if err != nil {
		return fmt.Errorf("%w：无法定位可信的 GitHub CLI", ErrUnverified)
	}
	tool, err = filepath.Abs(tool)
	if err != nil {
		return fmt.Errorf("%w：GitHub CLI 路径无效", ErrUnverified)
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return fmt.Errorf("%w：待验目录无效", ErrUnverified)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("%w：待验目录无效", ErrUnverified)
	}
	if within(root, tool) {
		return fmt.Errorf("%w：不能使用待验目录内的 GitHub CLI", ErrUnverified)
	}
	command := exec.CommandContext(ctx, tool, args...)
	command.Dir = filepath.Dir(artifact)
	if err := command.Run(); err != nil {
		// Do not forward arbitrary tool output or credentials to CLI/JSON logs.
		return fmt.Errorf("%w：证明、签发身份或验证服务检查失败；保留原版本并检查网络和可信 GitHub CLI", ErrUnverified)
	}
	return nil
}
