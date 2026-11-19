package releasetrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return keyID(pub), priv
}

func signedTestMetadata(t *testing.T, role, id string, priv ed25519.PrivateKey, body any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return signEnvelope(role, id, raw, priv)
}

func TestUpdateMetadata_shouldAcceptSignedTarget_whenFreshAndMonotonic(t *testing.T) {
	id, priv := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root := RootMetadata{Version: 2, Expires: now.Add(365 * 24 * time.Hour), Keys: map[string]PublicKey{id: publicKey(priv)}, Roles: map[string]Role{"targets": {KeyIDs: []string{id}, Threshold: 1}}}
	targets := TargetsMetadata{Version: 4, Expires: now.Add(7 * 24 * time.Hour), Targets: map[string]Target{"runtime.zip": {SHA256: "a" + strings.Repeat("0", 63), Length: 12}}}
	if _, err := VerifyTargets(signedTestMetadata(t, "targets", id, priv, targets), root, 3, now); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateMetadata_shouldRejectExpiryRollbackRevocationAndWrongDigest(t *testing.T) {
	id, priv := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root := RootMetadata{Version: 2, Expires: now.Add(365 * 24 * time.Hour), Keys: map[string]PublicKey{id: publicKey(priv)}, Roles: map[string]Role{"targets": {KeyIDs: []string{id}, Threshold: 1}}}
	base := TargetsMetadata{Version: 4, Expires: now.Add(time.Hour), Targets: map[string]Target{"runtime.zip": {SHA256: "a" + strings.Repeat("0", 63), Length: 12}}}
	for name, tc := range map[string]struct {
		body TargetsMetadata
		min  int
		at   time.Time
		want error
	}{
		"expired":  {body: base, min: 3, at: now.Add(2 * time.Hour), want: ErrMetadataExpired},
		"rollback": {body: TargetsMetadata{Version: 2, Expires: base.Expires, Targets: base.Targets}, min: 3, at: now, want: ErrMetadataRollback},
		"revoked":  {body: TargetsMetadata{Version: 4, Expires: base.Expires, Targets: base.Targets, Revoked: true}, min: 3, at: now, want: ErrMetadataRevoked},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := VerifyTargets(signedTestMetadata(t, "targets", id, priv, tc.body), root, tc.min, tc.at)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v got %v", tc.want, err)
			}
		})
	}
	bad := base
	bad.Targets = map[string]Target{"runtime.zip": {SHA256: "b" + strings.Repeat("0", 63), Length: 12}}
	if _, err := VerifyTargetBytes(signedTestMetadata(t, "targets", id, priv, base), root, 3, now, "runtime.zip", []byte("bytes")); !errors.Is(err, ErrTargetMismatch) {
		t.Fatal("wrong target digest accepted", err)
	}
	_ = bad
}

func TestUpdateMetadata_shouldRequireThresholdAndRejectTampering(t *testing.T) {
	id1, priv1 := testKey(t)
	id2, priv2 := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root := RootMetadata{Version: 1, Expires: now.Add(time.Hour), Keys: map[string]PublicKey{id1: publicKey(priv1), id2: publicKey(priv2)}, Roles: map[string]Role{"targets": {KeyIDs: []string{id1, id2}, Threshold: 2}}}
	targets := TargetsMetadata{Version: 1, Expires: now.Add(time.Hour), Targets: map[string]Target{}}
	raw, _ := json.Marshal(targets)
	envelope := metadataEnvelope{Role: "targets", Signed: raw, Signatures: []metadataSignature{{KeyID: id1, Sig: base64.RawStdEncoding.EncodeToString(ed25519.Sign(priv1, signedPayload("targets", raw)))}}}
	one, _ := json.Marshal(envelope)
	if _, err := VerifyTargets(one, root, 1, now); !errors.Is(err, ErrMetadataInvalid) {
		t.Fatal("single signature met threshold", err)
	}
	two := signEnvelope("targets", id1, raw, priv1)
	var parsed metadataEnvelope
	_ = json.Unmarshal(two, &parsed)
	parsed.Signatures = append(parsed.Signatures, metadataSignature{KeyID: id2, Sig: base64.RawStdEncoding.EncodeToString(ed25519.Sign(priv2, signedPayload("targets", raw)))})
	two, _ = json.Marshal(parsed)
	if _, err := VerifyTargets(two, root, 1, now); err != nil {
		t.Fatal("valid threshold rejected", err)
	}
	parsed.Signed = []byte(`{"version":1,"expires":"2026-11-17T01:00:00Z","targets":{"evil":{"sha256":"` + strings.Repeat("0", 64) + `","length":0}}}`)
	tampered, _ := json.Marshal(parsed)
	if _, err := VerifyTargets(tampered, root, 1, now); !errors.Is(err, ErrMetadataInvalid) {
		t.Fatal("tampered signed body accepted", err)
	}
}

func TestUpdateMetadata_shouldRequireCrossSignedRootRotation(t *testing.T) {
	oldID, oldPriv := testKey(t)
	newID, newPriv := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	old := RootMetadata{Version: 1, Expires: now.Add(365 * 24 * time.Hour), Keys: map[string]PublicKey{oldID: publicKey(oldPriv)}, Roles: map[string]Role{"root": {KeyIDs: []string{oldID}, Threshold: 1}}}
	rotated := RootMetadata{Version: 2, Expires: now.Add(365 * 24 * time.Hour), Keys: map[string]PublicKey{newID: publicKey(newPriv)}, Roles: map[string]Role{"root": {KeyIDs: []string{newID}, Threshold: 1}}, RevokedKeys: []string{oldID}}
	rawSigned, _ := json.Marshal(rotated)
	envelope := metadataEnvelope{Role: "root", Signed: rawSigned, Signatures: []metadataSignature{
		{KeyID: oldID, Sig: base64.RawStdEncoding.EncodeToString(ed25519.Sign(oldPriv, signedPayload("root", rawSigned)))},
		{KeyID: newID, Sig: base64.RawStdEncoding.EncodeToString(ed25519.Sign(newPriv, signedPayload("root", rawSigned)))},
	}}
	raw, _ := json.Marshal(envelope)
	if got, err := VerifyRoot(raw, old, now); err != nil || got.Version != 2 || len(got.RevokedKeys) != 1 {
		t.Fatalf("cross-signed root rejected: %+v %v", got, err)
	}
	for _, signatures := range [][]metadataSignature{{envelope.Signatures[0]}, {envelope.Signatures[1]}} {
		envelope.Signatures = signatures
		raw, _ = json.Marshal(envelope)
		if _, err := VerifyRoot(raw, old, now); !errors.Is(err, ErrMetadataInvalid) {
			t.Fatalf("single-sided root accepted: %v", err)
		}
	}
}

func TestUpdateMetadata_shouldBindSnapshotAndTimestampVersions(t *testing.T) {
	id, priv := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root := RootMetadata{Version: 1, Expires: now.Add(time.Hour), Keys: map[string]PublicKey{id: publicKey(priv)}, Roles: map[string]Role{"snapshot": {KeyIDs: []string{id}, Threshold: 1}, "timestamp": {KeyIDs: []string{id}, Threshold: 1}}}
	targetsRaw := signedTestMetadata(t, "targets", id, priv, TargetsMetadata{Version: 3, Expires: now.Add(time.Hour), Targets: map[string]Target{}})
	snapshot := SnapshotMetadata{Version: 4, Expires: now.Add(time.Hour), Meta: map[string]MetaFile{"targets.json": {Version: 3, Length: int64(len(targetsRaw)), SHA256: digestBytes(targetsRaw)}}}
	snapshotRaw := signedTestMetadata(t, "snapshot", id, priv, snapshot)
	if _, err := VerifySnapshot(snapshotRaw, root, 4, now, "targets.json", targetsRaw); err != nil {
		t.Fatal("snapshot rejected", err)
	}
	timestamp := TimestampMetadata{Version: 5, Expires: now.Add(time.Hour), Snapshot: MetaFile{Version: 4, Length: int64(len(snapshotRaw)), SHA256: digestBytes(snapshotRaw)}}
	if _, err := VerifyTimestamp(signedTestMetadata(t, "timestamp", id, priv, timestamp), root, 5, now, snapshotRaw); err != nil {
		t.Fatal("timestamp rejected", err)
	}
	if _, err := VerifyTimestamp(signedTestMetadata(t, "timestamp", id, priv, timestamp), root, 5, now, append(snapshotRaw, 'x')); !errors.Is(err, ErrTargetMismatch) {
		t.Fatal("timestamp accepted changed snapshot", err)
	}
}

func TestUpdateMetadata_shouldVerifyCompleteBundleChain(t *testing.T) {
	id, priv := testKey(t)
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	root := RootMetadata{Version: 1, Expires: now.Add(time.Hour), Keys: map[string]PublicKey{id: publicKey(priv)}, Roles: map[string]Role{"targets": {KeyIDs: []string{id}, Threshold: 1}, "snapshot": {KeyIDs: []string{id}, Threshold: 1}, "timestamp": {KeyIDs: []string{id}, Threshold: 1}}}
	targets := signedTestMetadata(t, "targets", id, priv, TargetsMetadata{Version: 3, Expires: now.Add(time.Hour), Targets: map[string]Target{}})
	snapshot := signedTestMetadata(t, "snapshot", id, priv, SnapshotMetadata{Version: 4, Expires: now.Add(time.Hour), Meta: map[string]MetaFile{"targets.json": {Version: 3, Length: int64(len(targets)), SHA256: digestBytes(targets)}}})
	timestamp := signedTestMetadata(t, "timestamp", id, priv, TimestampMetadata{Version: 5, Expires: now.Add(time.Hour), Snapshot: MetaFile{Version: 4, Length: int64(len(snapshot)), SHA256: digestBytes(snapshot)}})
	verified, err := VerifyBundle(MetadataBundle{Timestamp: timestamp, Snapshot: snapshot, Targets: targets}, root, 1, 5, 4, 3, now)
	if err != nil || verified.Targets.Version != 3 {
		t.Fatalf("bundle rejected: %+v %v", verified, err)
	}
	broken := snapshot[:len(snapshot)-1]
	if _, err := VerifyBundle(MetadataBundle{Timestamp: timestamp, Snapshot: broken, Targets: targets}, root, 1, 5, 4, 3, now); !errors.Is(err, ErrTargetMismatch) && !errors.Is(err, ErrMetadataInvalid) {
		t.Fatalf("broken chain returned unexpected error: %v", err)
	}
}
