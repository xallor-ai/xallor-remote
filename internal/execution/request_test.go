package execution

import (
	"errors"
	"strings"
	"testing"
)

// The final contract accepts exactly one execution kind and rejects obsolete,
// unknown, malformed or excessive input before a Runtime can allocate a job.
func TestDecode_should_validateExclusiveExecutionInputs(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"script", `{"script":{"text":"echo hi","shell":"powershell"}}`, true},
		{"program", `{"program":{"executable":"git","args":["","a b","$HOME"]}}`, true},
		{"zero args", `{"program":{"executable":"whoami"}}`, true},
		{"paired unicode", `{"program":{"executable":"x","args":["\ud83c\udf0d"]}}`, true},
		{"literal escape", `{"script":{"text":"echo \\ud800"}}`, true},
		{"unpaired high", `{"program":{"executable":"x","args":["\ud800"]}}`, false},
		{"unpaired low", `{"script":{"text":"\udfff"}}`, false},
		{"wrong pair", `{"script":{"text":"\ud800\u0041"}}`, false},
		{"null argument", `{"program":{"executable":"x","args":[null]}}`, false},
		{"null mode", `{"script":null,"program":{"executable":"x"}}`, false},
		{"null timeout", `{"script":{"text":"x"},"timeout_ms":null}`, false},
		{"literal null", `{"script":{"text":"echo null"}}`, true},
		{"obsolete", `{"command":"echo no"}`, false},
		{"both", `{"script":{"text":"echo no"},"program":{"executable":"git"}}`, false},
		{"neither", `{}`, false},
		{"unknown nested", `{"script":{"text":"x","env":{}}}`, false},
		{"empty text", `{"script":{"text":""}}`, false},
		{"nul", `{"program":{"executable":"git","args":["\u0000"]}}`, false},
		{"negative timeout", `{"script":{"text":"x"},"timeout_ms":-1}`, false},
		{"long timeout", `{"script":{"text":"x"},"timeout_ms":3600001}`, false},
		{"shell path", `{"script":{"text":"x","shell":"/bin/bash"}}`, false},
		{"trailing", `{"script":{"text":"x"}} {}`, false},
		{"malformed", `{"script":{"text":"x"},`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode([]byte(tc.raw)); (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
	if _, err := Decode([]byte("{\"script\":{\"text\":\"\xff\"}}")); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid raw UTF-8 accepted", err)
	}
	if err := (Request{Script: &Script{Text: strings.Repeat("中", MaxInputBytes/3+1)}}).Validate(); !errors.Is(err, ErrTooLarge) {
		t.Fatal("byte limit not enforced", err)
	}
	if err := (Request{Program: &Program{Executable: "git", Args: make([]string, MaxArguments+1)}}).Validate(); !errors.Is(err, ErrTooLarge) {
		t.Fatal("argument limit not enforced", err)
	}
}
