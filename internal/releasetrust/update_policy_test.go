package releasetrust

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRoot(t *testing.T, now time.Time) (RootMetadata, string, ed25519.PrivateKey) {
	id, priv := testKey(t)
	return RootMetadata{Version: 1, Expires: now.Add(365 * 24 * time.Hour), Keys: map[string]PublicKey{id: publicKey(priv)}, Roles: map[string]Role{"targets": {KeyIDs: []string{id}, Threshold: 1}, "snapshot": {KeyIDs: []string{id}, Threshold: 1}, "timestamp": {KeyIDs: []string{id}, Threshold: 1}}}, id, priv
}

func testBundle(t *testing.T, id string, priv ed25519.PrivateKey, now time.Time, rootVersion, timestampVersion, snapshotVersion, targetsVersion int) (MetadataBundle, MetadataVersions) {
	targets := signedTestMetadata(t, "targets", id, priv, TargetsMetadata{Version: targetsVersion, Expires: now.Add(time.Hour), Targets: map[string]Target{}})
	snapshot := signedTestMetadata(t, "snapshot", id, priv, SnapshotMetadata{Version: snapshotVersion, Expires: now.Add(time.Hour), Meta: map[string]MetaFile{"targets.json": {Version: targetsVersion, Length: int64(len(targets)), SHA256: digestBytes(targets)}}})
	timestamp := signedTestMetadata(t, "timestamp", id, priv, TimestampMetadata{Version: timestampVersion, Expires: now.Add(time.Hour), Snapshot: MetaFile{Version: snapshotVersion, Length: int64(len(snapshot)), SHA256: digestBytes(snapshot)}})
	return MetadataBundle{Timestamp: timestamp, Snapshot: snapshot, Targets: targets}, MetadataVersions{Root: rootVersion, Timestamp: timestampVersion, Snapshot: snapshotVersion, Targets: targetsVersion}
}

func TestMetadataPolicy_shouldPersistAcceptedVersionsAndRejectRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata-policy.json")
	policy := OpenMetadataPolicy(path)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root, id, priv := testRoot(t, now)
	bundle, versions := testBundle(t, id, priv, now, 1, 2, 3, 4)
	if got, err := policy.Accept(bundle, root, now); err != nil || got != versions {
		t.Fatalf("accept failed: %+v %v", got, err)
	}
	reopened := OpenMetadataPolicy(path)
	if got, err := reopened.State(); err != nil || got != versions {
		t.Fatalf("state did not persist: %+v %v", got, err)
	}
	older, _ := testBundle(t, id, priv, now, 1, 1, 2, 3)
	if _, err := reopened.Accept(older, root, now); !errors.Is(err, ErrMetadataRollback) {
		t.Fatal("older metadata accepted", err)
	}
	other, _, _ := testRoot(t, now)
	if _, err := reopened.Accept(bundle, other, now); !errors.Is(err, ErrMetadataInvalid) {
		t.Fatal("foreign root accepted", err)
	}
}

func TestMetadataPolicy_shouldFailClosedOnCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata-policy.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"root":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMetadataPolicy(path).State(); !errors.Is(err, ErrMetadataInvalid) {
		t.Fatal("corrupt policy accepted", err)
	}
}
