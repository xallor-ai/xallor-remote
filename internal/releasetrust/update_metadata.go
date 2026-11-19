package releasetrust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrMetadataInvalid  = errors.New("update metadata is invalid")
	ErrMetadataExpired  = errors.New("update metadata is expired")
	ErrMetadataRollback = errors.New("update metadata version rolls back")
	ErrMetadataRevoked  = errors.New("update metadata is revoked")
	ErrTargetMismatch   = errors.New("update target does not match metadata")
)

type PublicKey string

type Role struct {
	KeyIDs    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

type RootMetadata struct {
	Version     int                  `json:"version"`
	Expires     time.Time            `json:"expires"`
	Keys        map[string]PublicKey `json:"keys"`
	Roles       map[string]Role      `json:"roles"`
	RevokedKeys []string             `json:"revoked_keys,omitempty"`
}

type Target struct {
	SHA256 string `json:"sha256"`
	Length int64  `json:"length"`
}

type TargetsMetadata struct {
	Version int               `json:"version"`
	Expires time.Time         `json:"expires"`
	Revoked bool              `json:"revoked,omitempty"`
	Targets map[string]Target `json:"targets"`
}

type MetaFile struct {
	Version int    `json:"version"`
	Length  int64  `json:"length"`
	SHA256  string `json:"sha256"`
}

type SnapshotMetadata struct {
	Version int                 `json:"version"`
	Expires time.Time           `json:"expires"`
	Revoked bool                `json:"revoked,omitempty"`
	Meta    map[string]MetaFile `json:"meta"`
}

type TimestampMetadata struct {
	Version  int       `json:"version"`
	Expires  time.Time `json:"expires"`
	Revoked  bool      `json:"revoked,omitempty"`
	Snapshot MetaFile  `json:"snapshot"`
}

type MetadataBundle struct {
	Root      []byte
	Timestamp []byte
	Snapshot  []byte
	Targets   []byte
}

type VerifiedMetadata struct {
	Root      RootMetadata
	Timestamp TimestampMetadata
	Snapshot  SnapshotMetadata
	Targets   TargetsMetadata
}

type metadataSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

type metadataEnvelope struct {
	Role       string              `json:"role"`
	Signed     json.RawMessage     `json:"signed"`
	Signatures []metadataSignature `json:"signatures"`
}

func keyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func publicKey(private ed25519.PrivateKey) PublicKey {
	return PublicKey(base64.RawStdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)))
}

func signEnvelope(role, id string, signed []byte, private ed25519.PrivateKey) []byte {
	payload := signedPayload(role, signed)
	envelope := metadataEnvelope{Role: role, Signed: signed, Signatures: []metadataSignature{{KeyID: id, Sig: base64.RawStdEncoding.EncodeToString(ed25519.Sign(private, payload))}}}
	raw, _ := json.Marshal(envelope)
	return raw
}

func signedPayload(role string, signed []byte) []byte {
	return append(append([]byte(role+"\n"), signed...), '\n')
}

func VerifyTargets(raw []byte, root RootMetadata, minimumVersion int, now time.Time) (TargetsMetadata, error) {
	var parsed TargetsMetadata
	if err := verifyRole(raw, "targets", root, minimumVersion, now, &parsed); err != nil {
		return TargetsMetadata{}, err
	}
	return parsed, nil
}

// VerifyRoot rotates a root only when the new root is signed by both the
// currently trusted root role and the new root role. This prevents a single
// compromised successor key from silently replacing the trust root.
func VerifyRoot(raw []byte, current RootMetadata, now time.Time) (RootMetadata, error) {
	if current.Version < 1 || current.Expires.Before(now) {
		return RootMetadata{}, ErrMetadataExpired
	}
	var envelope metadataEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Role != "root" || len(envelope.Signed) == 0 {
		return RootMetadata{}, ErrMetadataInvalid
	}
	var next RootMetadata
	if json.Unmarshal(envelope.Signed, &next) != nil || next.Version != current.Version+1 || next.Expires.Before(now) || next.Expires.IsZero() {
		return RootMetadata{}, ErrMetadataRollback
	}
	if err := validateRoot(next); err != nil {
		return RootMetadata{}, err
	}
	if err := verifySignatures(envelope, "root", current, current.Roles["root"]); err != nil {
		return RootMetadata{}, err
	}
	if err := verifySignatures(envelope, "root", next, next.Roles["root"]); err != nil {
		return RootMetadata{}, err
	}
	return next, nil
}

func VerifyTargetBytes(raw []byte, root RootMetadata, minimumVersion int, now time.Time, name string, content []byte) (Target, error) {
	targets, err := VerifyTargets(raw, root, minimumVersion, now)
	if err != nil {
		return Target{}, err
	}
	target, ok := targets.Targets[name]
	if !ok || target.Length != int64(len(content)) {
		return Target{}, ErrTargetMismatch
	}
	sum := sha256.Sum256(content)
	if target.SHA256 != hex.EncodeToString(sum[:]) {
		return Target{}, ErrTargetMismatch
	}
	return target, nil
}

func VerifySnapshot(raw []byte, root RootMetadata, minimumVersion int, now time.Time, targetsName string, targetsRaw []byte) (SnapshotMetadata, error) {
	var snapshot SnapshotMetadata
	if err := verifyRole(raw, "snapshot", root, minimumVersion, now, &snapshot); err != nil {
		return SnapshotMetadata{}, err
	}
	meta, ok := snapshot.Meta[targetsName]
	if !ok || meta.Length != int64(len(targetsRaw)) || meta.SHA256 != digestBytes(targetsRaw) {
		return SnapshotMetadata{}, ErrTargetMismatch
	}
	return snapshot, nil
}

func VerifyTimestamp(raw []byte, root RootMetadata, minimumVersion int, now time.Time, snapshotRaw []byte) (TimestampMetadata, error) {
	var timestamp TimestampMetadata
	if err := verifyRole(raw, "timestamp", root, minimumVersion, now, &timestamp); err != nil {
		return TimestampMetadata{}, err
	}
	if timestamp.Snapshot.Length != int64(len(snapshotRaw)) || timestamp.Snapshot.SHA256 != digestBytes(snapshotRaw) {
		return TimestampMetadata{}, ErrTargetMismatch
	}
	return timestamp, nil
}

// VerifyBundle checks the complete signed metadata chain without touching a
// target file or persisting trust state. Callers supply previously accepted
// minimum versions; a future installer must persist them atomically.
func VerifyBundle(bundle MetadataBundle, current RootMetadata, minRoot, minTimestamp, minSnapshot, minTargets int, now time.Time) (VerifiedMetadata, error) {
	root := current
	if len(bundle.Root) != 0 {
		var err error
		root, err = VerifyRoot(bundle.Root, current, now)
		if err != nil {
			return VerifiedMetadata{}, err
		}
	}
	if root.Version < minRoot {
		return VerifiedMetadata{}, ErrMetadataRollback
	}
	timestamp, err := VerifyTimestamp(bundle.Timestamp, root, minTimestamp, now, bundle.Snapshot)
	if err != nil {
		return VerifiedMetadata{}, err
	}
	snapshot, err := VerifySnapshot(bundle.Snapshot, root, minSnapshot, now, "targets.json", bundle.Targets)
	if err != nil {
		return VerifiedMetadata{}, err
	}
	targets, err := VerifyTargets(bundle.Targets, root, minTargets, now)
	if err != nil {
		return VerifiedMetadata{}, err
	}
	if timestamp.Snapshot.Version != snapshot.Version || snapshot.Meta["targets.json"].Version != targets.Version {
		return VerifiedMetadata{}, ErrMetadataRollback
	}
	return VerifiedMetadata{Root: root, Timestamp: timestamp, Snapshot: snapshot, Targets: targets}, nil
}

func verifyRole(raw []byte, expectedRole string, root RootMetadata, minimumVersion int, now time.Time, output any) error {
	if root.Version < 1 || now.IsZero() || root.Expires.Before(now) {
		return ErrMetadataExpired
	}
	role, ok := root.Roles[expectedRole]
	if !ok || role.Threshold < 1 || role.Threshold > len(role.KeyIDs) {
		return ErrMetadataInvalid
	}
	var envelope metadataEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Role != expectedRole || len(envelope.Signed) == 0 {
		return ErrMetadataInvalid
	}
	var versioned struct {
		Version int       `json:"version"`
		Expires time.Time `json:"expires"`
		Revoked bool      `json:"revoked,omitempty"`
	}
	if json.Unmarshal(envelope.Signed, &versioned) != nil {
		return ErrMetadataInvalid
	}
	if versioned.Version < minimumVersion {
		return ErrMetadataRollback
	}
	if versioned.Expires.Before(now) {
		return ErrMetadataExpired
	}
	if versioned.Revoked {
		return ErrMetadataRevoked
	}
	if json.Unmarshal(envelope.Signed, output) != nil {
		return ErrMetadataInvalid
	}
	if err := verifySignatures(envelope, expectedRole, root, role); err != nil {
		return err
	}
	if targets, ok := output.(*TargetsMetadata); ok && targets.Targets == nil {
		return ErrMetadataInvalid
	}
	return nil
}

func validateRoot(root RootMetadata) error {
	if root.Version < 1 || root.Expires.IsZero() || len(root.Keys) == 0 || len(root.Roles) == 0 {
		return ErrMetadataInvalid
	}
	revoked := map[string]struct{}{}
	for _, id := range root.RevokedKeys {
		revoked[id] = struct{}{}
		if _, ok := root.Keys[id]; ok {
			return ErrMetadataInvalid
		}
	}
	for _, role := range root.Roles {
		if role.Threshold < 1 || role.Threshold > len(role.KeyIDs) {
			return ErrMetadataInvalid
		}
		for _, id := range role.KeyIDs {
			if _, ok := revoked[id]; ok {
				return ErrMetadataRevoked
			}
			encoded, err := base64.RawStdEncoding.DecodeString(string(root.Keys[id]))
			if err != nil || len(encoded) != ed25519.PublicKeySize || keyID(ed25519.PublicKey(encoded)) != id {
				return ErrMetadataInvalid
			}
		}
	}
	return nil
}

func verifySignatures(envelope metadataEnvelope, expectedRole string, root RootMetadata, role Role) error {
	allowed := map[string]struct{}{}
	for _, id := range role.KeyIDs {
		allowed[id] = struct{}{}
	}
	valid := map[string]struct{}{}
	for _, sig := range envelope.Signatures {
		if _, ok := allowed[sig.KeyID]; !ok {
			continue
		}
		if _, duplicate := valid[sig.KeyID]; duplicate {
			continue
		}
		encoded, err := base64.RawStdEncoding.DecodeString(string(root.Keys[sig.KeyID]))
		if err != nil || len(encoded) != ed25519.PublicKeySize {
			return ErrMetadataInvalid
		}
		signature, err := base64.RawStdEncoding.DecodeString(sig.Sig)
		if err != nil || !ed25519.Verify(ed25519.PublicKey(encoded), signedPayload(expectedRole, envelope.Signed), signature) {
			continue
		}
		valid[sig.KeyID] = struct{}{}
	}
	if len(valid) < role.Threshold {
		return fmt.Errorf("%w: threshold not met", ErrMetadataInvalid)
	}
	return nil
}
