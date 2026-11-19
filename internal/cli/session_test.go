package cli

import (
	"strings"
	"testing"
	"time"
)

func TestBuildSessionRequestParams_shouldRejectMissingSecretOrAuthorityFields(t *testing.T) {
	base := sessionRequestOptions{DeviceID: "device", CodeID: "code", CodeSecret: "secret", TargetUID: "uid", Generation: 1, Scope: []string{"info"}}
	for name, mutate := range map[string]func(*sessionRequestOptions){
		"device":     func(v *sessionRequestOptions) { v.DeviceID = "" },
		"code":       func(v *sessionRequestOptions) { v.CodeID = "" },
		"secret":     func(v *sessionRequestOptions) { v.CodeSecret = "" },
		"target":     func(v *sessionRequestOptions) { v.TargetUID = "" },
		"generation": func(v *sessionRequestOptions) { v.Generation = 0 },
		"scope":      func(v *sessionRequestOptions) { v.Scope = nil },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if _, err := buildSessionRequestParams(value, time.Unix(100, 0)); err == nil {
				t.Fatal("invalid session request accepted")
			}
		})
	}
}

func TestBuildSessionRequestParams_shouldUseAuthorityRelativeExpiry_andKeepSecretOnlyInParams(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	params, err := buildSessionRequestParams(sessionRequestOptions{DeviceID: "device", CodeID: "code", CodeSecret: "secret", TargetUID: "uid", Generation: 2, Scope: []string{"info", "exec"}, ExpiresIn: 5 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := params["session_expires_at"]; got != now.Add(5*time.Minute).Unix() {
		t.Fatalf("expiry=%v", got)
	}
	if params["code_secret"] != "secret" || !strings.Contains(params["code_secret"].(string), "secret") {
		t.Fatal("secret was not passed to Runtime request")
	}
}

func TestSessionCommand_shouldValidateBeforeRuntimeEnsure(t *testing.T) {
	ensures := 0
	calls := 0
	command := cmdSessionWith(func() error {
		ensures++
		return nil
	}, func(string, map[string]any) (map[string]any, error) {
		calls++
		return map[string]any{}, nil
	})
	command.SetArgs([]string{"request", "--device", "device", "--code-id", "code", "--target-uid", "uid", "--generation", "1", "--scope", "info"})
	if err := command.Execute(); err == nil {
		t.Fatal("missing access code was accepted")
	}
	if ensures != 0 || calls != 0 {
		t.Fatalf("invalid request caused side effects: ensures=%d calls=%d", ensures, calls)
	}
}

func TestSessionResumeCommand_shouldSendCapabilityOnlyAfterValidation(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_RESUME_CAPABILITY", "resume-secret")
	ensures := 0
	var method string
	var params map[string]any
	command := cmdSessionWith(func() error { ensures++; return nil }, func(got string, gotParams map[string]any) (map[string]any, error) {
		method, params = got, gotParams
		return map[string]any{"status": "active"}, nil
	})
	command.SetArgs([]string{"resume", "--device", "device", "--session-id", "session", "--resume-id", "resume", "--target-uid", "uid"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if ensures != 1 || method != "session.resume" || params["resume_capability"] != "resume-secret" {
		t.Fatalf("method=%s params=%v ensures=%d", method, params, ensures)
	}
}
