package accesscode

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// Canonicalization may remove duplicates, never add permissions or alias the
// caller's slice; stale generations and empty scopes cannot default to all.
func TestSessionScope_shouldValidateGenerationAndSubset_withoutMutatingInput(t *testing.T) {
	code := codeFixture(t, OneTime)
	requested := []string{"info", "info"}
	scope, err := code.ScopeForSession(code.Generation, requested)
	if err != nil || !reflect.DeepEqual(scope, []string{"info"}) {
		t.Fatalf("scope=%v err=%v", scope, err)
	}
	scope[0] = "exec"
	if requested[0] != "info" || !reflect.DeepEqual(code.Scope, []string{"info", "exec"}) {
		t.Fatal("scope normalization mutated inputs")
	}
	for _, tc := range []struct {
		generation uint64
		scope      []string
	}{
		{code.Generation - 1, []string{"info"}}, {code.Generation, nil},
		{code.Generation, []string{"write"}}, {code.Generation, []string{"info", ""}},
	} {
		if _, err := code.ScopeForSession(tc.generation, tc.scope); !errors.Is(err, ErrSessionScope) {
			t.Fatalf("invalid scope or generation accepted: %v", err)
		}
	}
}

// Approval of a subset must persist that subset; missing/expanded consent
// releases the reservation without consuming a one-time code.
func TestFinalize_shouldNeverExpandRecipientConsent(t *testing.T) {
	for _, requested := range [][]string{{"info"}, nil, {"write"}} {
		code := codeFixture(t, OneTime)
		reservation, err := code.Reserve("reservation", "session", accessCodeNow, accessCodeNow.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		next, held, session, err := Finalize(code, reservation, true, accessCodeNow.Add(time.Second), nil, requested)
		if len(requested) == 1 && requested[0] == "info" {
			if err != nil || !reflect.DeepEqual(session.Scope, []string{"info"}) || next.State != StateConsumed {
				t.Fatalf("scope=%v err=%v", session.Scope, err)
			}
		} else if !errors.Is(err, ErrSessionScope) || session.ID != "" || next.State != StateActive || held.State != ReservationReleased {
			t.Fatalf("invalid consent created authorization: code=%s reservation=%s err=%v", next.State, held.State, err)
		}
	}
}
