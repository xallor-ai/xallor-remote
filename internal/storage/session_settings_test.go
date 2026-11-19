package storage

import (
	"context"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/access"
	"github.com/xallor-ai/xallor-remote/internal/accesscode"
)

func settingsConsentFixture(t *testing.T, owned bool) (*SQLStore, access.Device, accesscode.ResumeCapability) {
	t.Helper()
	s, err := OpenSQLStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	d, err := s.RegisterDevice(ctx, "device", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	d.GrantHash = "grant-hash"
	d, err = s.UpdateDevice(ctx, "device", d)
	if err != nil {
		t.Fatal(err)
	}
	if d.SessionEpoch != "" {
		t.Fatal("plain settings implicitly enabled sessions")
	}
	if owned {
		owner := enrollment(t, s, "owner", "")
		d = manageAccess(t, s, d, owner.Identity, access.Request{Action: "claim"}).Device
	}
	proof := resumeStorageFixture()
	target := accesscode.TargetBinding{DeviceUID: d.UID, Epoch: "settings-epoch"}
	if err := s.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		if err := tx.ActivateSessionTarget(ctx, target, resumeStorageNow); err != nil {
			return err
		}
		code := accesscode.Code{ID: "settings-code", DeviceUID: d.UID, OwnershipID: d.OwnershipID, Generation: 1, Type: accesscode.LongLived, IssuedAt: resumeStorageNow, Scope: []string{"info"}, State: accesscode.StateActive}
		if err := tx.InsertCode(ctx, code, "hash"); err != nil {
			return err
		}
		if _, _, err := tx.ReserveCode(ctx, code.ID, "hash", "settings-request", proof.SessionID, resumeStorageNow, resumeStorageNow.Add(time.Minute), target); err != nil {
			return err
		}
		if _, _, _, err := tx.FinalizeReservation(ctx, "settings-request", true, resumeStorageNow, nil, code.Scope); err != nil {
			return err
		}
		return tx.InsertResume(ctx, proof)
	}); err != nil {
		t.Fatal(err)
	}
	d, err = s.LoadDevice(ctx, "device")
	if err != nil {
		t.Fatal(err)
	}
	return s, d, proof
}

// No-op settings and an unused sharing hash on an owned device must preserve
// current consent. Actual shared binding changes invalidate it atomically.
func TestSessionSettingsStorage_shouldChangeEpoch_onlyForEffectiveSettings(t *testing.T) {
	for _, kind := range []string{"same", "owned-grant-sync", "shared-grant-rotation", "inbound-off-on"} {
		t.Run(kind, func(t *testing.T) {
			s, before, proof := settingsConsentFixture(t, kind == "owned-grant-sync")
			ctx := context.Background()
			requested := before
			if kind == "owned-grant-sync" || kind == "shared-grant-rotation" {
				requested.GrantHash = "changed"
			}
			if kind == "inbound-off-on" {
				requested.Inbound = false
			}
			after, err := s.UpdateDevice(ctx, "device", requested)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "inbound-off-on" {
				if after.SessionEpochActive {
					t.Error("disabled epoch still active")
				}
				after.Inbound = true
				after, err = s.UpdateDevice(ctx, "device", after)
				if err != nil {
					t.Fatal(err)
				}
			}
			preserved := kind == "same" || kind == "owned-grant-sync"
			if (after.SessionEpoch == before.SessionEpoch) != preserved || !after.SessionEpochActive {
				t.Errorf("unexpected epoch change for %s", kind)
			}
			stored, err := s.LoadDevice(ctx, "device")
			if err != nil || stored != after {
				t.Fatal("returned snapshot differs from committed state", err)
			}
			if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
				ss, err := r.Session(ctx, proof.SessionID)
				if err != nil {
					return err
				}
				p, err := r.Resume(ctx, proof.ID)
				if err != nil {
					return err
				}
				if (ss.State == accesscode.SessionActive) != preserved || (p.State == accesscode.ResumeActive) != preserved {
					t.Error("consent mutation differs from setting")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A fault after the device write must roll back settings, epoch and consent
// together, rather than acknowledging disabled inbound with usable old proofs.
func TestSessionSettingsStorage_shouldRollbackEverything_whenConsentRetirementFails(t *testing.T) {
	s, before, proof := settingsConsentFixture(t, false)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER deny_settings_consent BEFORE UPDATE ON resume_capabilities BEGIN SELECT RAISE(ABORT,'fixture refusal'); END;`); err != nil {
		t.Fatal(err)
	}
	requested := before
	requested.Inbound = false
	requested.GrantHash = "changed"
	if _, err := s.UpdateDevice(ctx, "device", requested); err == nil {
		t.Fatal("partial settings committed")
	}
	after, err := s.LoadDevice(ctx, "device")
	if err != nil || after != before {
		t.Fatal("settings/epoch failed to roll back", err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		ss, err := r.Session(ctx, proof.SessionID)
		if err != nil {
			return err
		}
		if ss.State != accesscode.SessionActive {
			t.Error("session partially revoked")
		}
		p, err := r.Resume(ctx, proof.ID)
		if p.State != accesscode.ResumeActive {
			t.Error("resume partially revoked")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
