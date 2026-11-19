package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Unknown contents are evidence, not owned garbage; preserve the consume cause
// and refuse a manifest even when the consumer itself succeeded.
func TestSourceCleanup_should_retainUnexpectedFile_andPreserveCause(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "consume-error"}[fail], func(t *testing.T) {
			dir, _ := sourceBundle(t, "cleanup")
			cause := errors.New("consume refused")
			var extra string
			accept := func(context.Context, string, releasetrust.Identity, string, string) error { return nil }
			got, err := withSourceSnapshot(context.Background(), dir, "", accept, func(binary string, _ Manifest, _ []byte) error {
				extra = filepath.Join(filepath.Dir(binary), "unexpected")
				if e := os.WriteFile(extra, []byte("retain evidence"), 0600); e != nil {
					return e
				}
				if fail {
					return cause
				}
				return nil
			})
			if extra != "" {
				t.Cleanup(func() {
					_ = os.Remove(extra)
					_ = os.Remove(filepath.Join(filepath.Dir(extra), "manifest.json"))
					entries, _ := os.ReadDir(filepath.Dir(extra))
					for _, e := range entries {
						if !e.IsDir() {
							_ = os.Remove(filepath.Join(filepath.Dir(extra), e.Name()))
						}
					}
					_ = os.Remove(filepath.Dir(extra))
				})
			}
			if !errors.Is(err, ErrCleanupUncertain) || got != (Manifest{}) {
				t.Errorf("cleanup returned manifest=%+v error=%v", got, err)
			}
			if fail && !errors.Is(err, cause) {
				t.Errorf("lost consume error: %v", err)
			}
			if data, e := os.ReadFile(extra); e != nil || string(data) != "retain evidence" {
				t.Errorf("unexpected file removed: %q %v", data, e)
			}
		})
	}
}

// A proof refusal plus uncertain quarantine cleanup must precede installation
// writes and process calls. The injected verifier tests ordering, not crypto.
func TestSourceCleanup_should_preserveInstallState_whenPreflightCleanupFails(t *testing.T) {
	dir, m := sourceBundle(t, "preflight-cleanup")
	var extra string
	store := Store{Dir: filepath.Join(t.TempDir(), "absent"), sourceVerifier: func(_ context.Context, file string, _ releasetrust.Identity, _, _ string) error {
		extra = filepath.Join(filepath.Dir(file), "unexpected")
		if err := os.WriteFile(extra, []byte("evidence"), 0600); err != nil {
			return err
		}
		return releasetrust.ErrUnverified
	}}
	c := control(m)
	err := store.Install(dir, c)
	if extra != "" {
		t.Cleanup(func() {
			_ = os.Remove(extra)
			entries, _ := os.ReadDir(filepath.Dir(extra))
			for _, e := range entries {
				if !e.IsDir() {
					_ = os.Remove(filepath.Join(filepath.Dir(extra), e.Name()))
				}
			}
			_ = os.Remove(filepath.Dir(extra))
		})
	}
	if !errors.Is(err, ErrCleanupUncertain) || !errors.Is(err, releasetrust.ErrUnverified) {
		t.Errorf("lost uncertainty or refusal: %v", err)
	}
	if _, e := os.Stat(store.Dir); !os.IsNotExist(e) {
		t.Errorf("install directory mutated: %v", e)
	}
	if c.checks != 0 || c.stops != 0 || c.runningChecks != 0 {
		t.Fatal("preflight reached control")
	}
}

// Successful cleanup must not manufacture uncertainty on a normal proof denial.
func TestSourceCleanup_should_removeOwnedSnapshot_withoutFalseUncertainty(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "proof-denied"}[deny], func(t *testing.T) {
			dir, _ := sourceBundle(t, "normal-cleanup")
			var snapshot string
			got, err := withSourceSnapshot(context.Background(), dir, "", func(_ context.Context, file string, _ releasetrust.Identity, _, _ string) error {
				snapshot = filepath.Dir(file)
				if deny {
					return releasetrust.ErrUnverified
				}
				return nil
			}, nil)
			if errors.Is(err, ErrCleanupUncertain) || (!deny && err != nil) || (deny && !errors.Is(err, releasetrust.ErrUnverified)) {
				t.Fatal(err)
			}
			if deny && got != (Manifest{}) {
				t.Fatal("failed proof returned manifest")
			}
			if _, e := os.Stat(snapshot); !os.IsNotExist(e) {
				t.Fatalf("snapshot retained: %v", e)
			}
		})
	}
}

// Replacing a known member with another inode must retain both the replacement
// and original evidence, even though its pathname matches the receipt.
func TestSourceCleanup_should_retainReplacement_whenFileIdentityChanges(t *testing.T) {
	dir, _ := sourceBundle(t, "replaced")
	var path, moved string
	t.Cleanup(func() {
		if path != "" {
			_ = os.Remove(path)
			_ = os.Remove(moved)
			_ = os.Remove(filepath.Join(filepath.Dir(path), "manifest.json"))
			_ = os.Remove(filepath.Dir(path))
		}
	})
	got, err := withSourceSnapshot(context.Background(), dir, "", func(context.Context, string, releasetrust.Identity, string, string) error { return nil }, func(binary string, _ Manifest, _ []byte) error {
		path = binary
		moved = filepath.Join(t.TempDir(), "original")
		if err := os.Rename(path, moved); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("replacement"), 0600)
	})
	if !errors.Is(err, ErrCleanupUncertain) || got != (Manifest{}) {
		t.Fatal(got, err)
	}
	if data, e := os.ReadFile(path); e != nil || string(data) != "replacement" {
		t.Fatal("replacement removed", e)
	}
}

// A successful creation is owned before a write can fail; cleanup removes that
// partial member and its directory without recursive traversal.
func TestSourceCleanup_should_removePartialFile_whenWriteFails(t *testing.T) {
	snapshot, err := newSourceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	f, err := snapshot.create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("remainder")); err == nil {
		t.Fatal("expected closed descriptor write failure")
	}
	if err = snapshot.cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(snapshot.path); !os.IsNotExist(err) {
		t.Fatal("partial file retained", err)
	}
}

// A changed directory receipt cannot authorize deleting a replacement directory.
// The original root is retained and explicitly closed on this failure path.
func TestSourceCleanup_should_retainDirectory_whenIdentityChanges(t *testing.T) {
	snapshot, err := newSourceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	original := snapshot.path
	replacement := t.TempDir()
	snapshot.path = replacement // Simulate the pathname now naming another directory.
	if err = snapshot.cleanup(); err == nil {
		t.Fatal("changed directory accepted")
	}
	if _, err = os.Stat(replacement); err != nil {
		t.Fatal("replacement directory removed", err)
	}
	if err = os.Remove(original); err != nil {
		t.Fatal("original root handle not released", err)
	}
}
