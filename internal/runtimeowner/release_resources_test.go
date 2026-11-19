package runtimeowner

import (
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Value identities cannot be changed after intent; unknown rows cannot be closed again.
func TestReleaseResources_shouldBindIdentity_andRetainUnknown(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	r := releaseResources{transaction: tx}
	i := release.ResourceObligation(resourceIntentFixture("1"))
	if err := r.RecordIntent(i); err != nil {
		t.Fatal(err)
	}
	changed := i
	changed.SourceSnapshotSHA256 = strings.Repeat("f", 64)
	if err := r.Close(changed); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("changed receipt accepted", err)
	}
	if err := r.Unknown(i, CauseCleanupUnknown); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(i); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("unknown debt closed", err)
	}
	rows, err := j.Resources(request("1").ID)
	if err != nil || len(rows) != 1 || rows[0].Status != ResourceUnknown || rows[0].SourceSnapshotSHA256 != i.SourceSnapshotSHA256 {
		t.Fatal(rows, err)
	}
}

// A release caller cannot convert activation debt into selected retention by
// changing the value receipt; failed relabeling must preserve the original debt.
func TestReleaseResources_shouldPreserveActivationDebt_whenCallerRelabelsRetention(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	recorder := releaseResources{transaction: tx}
	original := release.ResourceObligation(resourceIntentFixture("1"))
	original.Kind = ResourceKindActivation
	original.LaunchID = ""
	selected := original
	selected.Retention = ResourceRetainSelected
	if err := recorder.RecordIntent(selected); !errors.Is(err, ErrInvalid) {
		t.Fatal("selection label admitted without authoritative receipt", err)
	}
	if err := recorder.RecordIntent(original); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(selected); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("changed retention closed activation debt", err)
	}
	if err := recorder.Unknown(selected, CauseCleanupUnknown); !errors.Is(err, ErrResourceConflict) {
		t.Fatal("changed retention mutated activation debt", err)
	}
	rows, err := j.Resources(request("1").ID)
	if err != nil || len(rows) != 1 || rows[0].ResourceObligation != ResourceObligation(original) || rows[0].Status != ResourceAllocated || rows[0].Revision != 1 {
		t.Fatalf("original debt changed: %+v %v", rows, err)
	}
	if !j.HasUnsettledResources(request("1").ID) {
		t.Fatal("activation debt treated as settled")
	}
	if err := tx.Finish(Rejected); !errors.Is(err, ErrUncertain) {
		t.Fatal("terminal operation erased activation debt", err)
	}
}
