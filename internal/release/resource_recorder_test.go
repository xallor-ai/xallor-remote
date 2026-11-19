package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type resourceTrace struct {
	intent, closed, unknown []ResourceObligation
	fail                    error
}

func (r *resourceTrace) RecordIntent(v ResourceObligation) error {
	r.intent = append(r.intent, v)
	return r.fail
}
func (r *resourceTrace) Close(v ResourceObligation) error {
	r.closed = append(r.closed, v)
	return r.fail
}
func (r *resourceTrace) Unknown(v ResourceObligation, _ string) error {
	r.unknown = append(r.unknown, v)
	return r.fail
}

// The source receipt precedes consumers and closes only after owned files disappear.
func TestSourceResource_shouldRecordBeforeConsumer_andCloseAfterCleanup(t *testing.T) {
	dir, m := sourceBundle(t, "resource")
	r := &resourceTrace{}
	s := mechanicsStore(t).WithResources(r).WithEffects(nil)
	s, _ = s.transition("requested", nil)
	var snapshot string
	got, err := s.withSourceSnapshot(context.Background(), dir, "", func(binary string, _ Manifest, raw []byte) error {
		snapshot = filepath.Dir(binary)
		if len(r.intent) != 1 || len(r.closed) != 0 {
			t.Fatal("intent not recorded before consumer")
		}
		i := r.intent[0]
		if i.ImageSHA256 != m.Artifact.SHA256 || i.SourceSnapshotSHA256 != snapshotDigest(raw, m.Artifact.SHA256) || i.ID == "" || i.AttemptID == "" || i.Generation != 1 {
			t.Fatalf("identity %+v", i)
		}
		return nil
	})
	if err != nil || got != m || len(r.closed) != 1 || len(r.unknown) != 0 || r.closed[0] != r.intent[0] {
		t.Fatal(got, err, r)
	}
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatal("closed before removal", err)
	}
}

// Unknown contents retain the receipt and preserve a simultaneous consume error.
func TestSourceResource_shouldKeepUnknownDebt_andConsumerCause(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "consume-error"}[fail], func(t *testing.T) {
			dir, _ := sourceBundle(t, "resource-unknown")
			r := &resourceTrace{}
			s := mechanicsStore(t).WithResources(r)
			cause := errors.New("consume failed")
			var snapshot string
			_, err := s.withSourceSnapshot(context.Background(), dir, "", func(binary string, _ Manifest, _ []byte) error {
				snapshot = filepath.Dir(binary)
				if err := os.WriteFile(filepath.Join(snapshot, "unknown"), []byte("retain"), 0600); err != nil {
					return err
				}
				if fail {
					return cause
				}
				return nil
			})
			t.Cleanup(func() {
				entries, _ := os.ReadDir(snapshot)
				for _, e := range entries {
					_ = os.Remove(filepath.Join(snapshot, e.Name()))
				}
				_ = os.Remove(snapshot)
			})
			if !errors.Is(err, ErrCleanupUncertain) || (fail && !errors.Is(err, cause)) || len(r.closed) != 0 || len(r.unknown) != 1 || r.unknown[0] != r.intent[0] {
				t.Fatal(err, r)
			}
			if _, err := os.Stat(filepath.Join(snapshot, "unknown")); err != nil {
				t.Fatal("unknown deleted", err)
			}
		})
	}
}

// Failure to persist intent prevents verifier/consumer dispatch and fails closed.
func TestSourceResource_shouldRefuseConsumer_whenIntentCannotPersist(t *testing.T) {
	dir, _ := sourceBundle(t, "resource-refused")
	cause := errors.New("ledger unavailable")
	r := &resourceTrace{fail: cause}
	s := mechanicsStore(t).WithResources(r)
	called := false
	_, err := s.withSourceSnapshot(context.Background(), dir, "", func(string, Manifest, []byte) error { called = true; return nil })
	if called || !errors.Is(err, cause) || !errors.Is(err, ErrCleanupUncertain) || len(r.closed) != 0 || len(r.unknown) != 0 {
		t.Fatal(err, r)
	}
}

// No recorder preserves the original read/consume/cleanup behavior and fresh
// snapshots in one recorded operation receive separate attempt identities.
func TestSourceResource_shouldKeepStandaloneBehavior_andSeparateAttempts(t *testing.T) {
	dir, m := sourceBundle(t, "source-attempts")
	s := mechanicsStore(t)
	if got, err := s.withSourceSnapshot(context.Background(), dir, "", nil); err != nil || got != m {
		t.Fatal(got, err)
	}
	r := &resourceTrace{}
	s = s.WithResources(r)
	for i := 0; i < 2; i++ {
		if _, err := s.withSourceSnapshot(context.Background(), dir, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.intent) != 2 || r.intent[0].ID == r.intent[1].ID || r.intent[0].AttemptID == r.intent[1].AttemptID || r.intent[0].SourceSnapshotSHA256 != r.intent[1].SourceSnapshotSHA256 {
		t.Fatal(r)
	}
}

// A failed durable close cannot be reported as success even after receipt-based
// deletion succeeded; the ledger remains allocated rather than inventing closure.
func TestSourceResource_shouldReturnUncertain_whenClosureCannotPersist(t *testing.T) {
	dir, _ := sourceBundle(t, "close-write")
	r := &resourceTrace{}
	cause := errors.New("close write failed")
	_, err := mechanicsStore(t).WithResources(r).withSourceSnapshot(context.Background(), dir, "", func(string, Manifest, []byte) error { r.fail = cause; return nil })
	if !errors.Is(err, ErrCleanupUncertain) || !errors.Is(err, cause) || len(r.closed) != 1 {
		t.Fatal(err, r)
	}
}
