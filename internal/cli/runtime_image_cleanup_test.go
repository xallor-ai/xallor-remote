package cli

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

type failedPreparedImage struct{ closes int }

func (f *failedPreparedImage) Close() error {
	f.closes++
	return errors.New("image handle release failed")
}

// Even when launch admission fails before a child starts, failed release of the
// prepared image must not be reported as a clean, restorable startup failure.
func TestRuntimeStartReportsUncertainUnstartedImageCleanup(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	image := &failedPreparedImage{}
	c := runtimeControl{prepareImage: func(*exec.Cmd, string) (io.Closer, error) { return image, nil }}
	err := c.Start(filepath.Join(t.TempDir(), "unstarted"), strings.Repeat("a", 64))
	if !errors.Is(err, release.ErrCleanupUncertain) || image.closes != 1 {
		t.Fatal(image.closes, err)
	}
}

// Probe failure/cancellation also cannot discard failure to release its image;
// callers must retain the uncertain lifecycle instead of treating it as denied.
func TestRuntimeProbePreservesImageCleanupUncertainty(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		image := &failedPreparedImage{}
		_, err := probeRuntimeIdentityWithImage(ctx, filepath.Join(t.TempDir(), "missing"), strings.Repeat("a", 64), func(*exec.Cmd, string) (io.Closer, error) { return image, nil })
		cancel()
		if !errors.Is(err, release.ErrCleanupUncertain) || image.closes != 1 {
			t.Fatal(cancelled, image.closes, err)
		}
	}
}

// Preparation itself may fail before returning a file; both control entry
// points must preserve its cleanup classification for the owner recorder.
func TestRuntimeControlsPreservePrepareCleanupUncertainty(t *testing.T) {
	prepare := func(*exec.Cmd, string) (io.Closer, error) {
		return nil, errors.Join(errors.New("prepare failed"), runtimeimage.ErrCleanupUncertain)
	}
	path := filepath.Join(t.TempDir(), "not-executed")
	if err := (runtimeControl{prepareImage: prepare}).Start(path, strings.Repeat("a", 64)); !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
	if _, err := probeRuntimeIdentityWithImage(context.Background(), path, strings.Repeat("a", 64), prepare); !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
}
