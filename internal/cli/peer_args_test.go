package cli

import (
	"io"
	"strings"
	"testing"
)

// The public CLI uses one target flag for all peer mutations; the IPC field
// remains device_id and no legacy --id compatibility is retained pre-release.
func TestPeerCommands_should_useDeviceFlag_consistently(t *testing.T) {
	root := cmdPeer()
	for _, name := range []string{"add", "remove", "rename"} {
		var commandName string
		for _, command := range root.Commands() {
			if command.Name() == name {
				commandName = name
				if command.Flags().Lookup("device") == nil {
					t.Fatalf("%s missing --device", name)
				}
				if command.Flags().Lookup("id") != nil {
					t.Fatalf("%s retained --id", name)
				}
			}
		}
		if commandName == "" {
			t.Fatal("missing", name)
		}
	}
	help := root.UsageString()
	if !strings.Contains(help, "peer") {
		t.Fatal("peer help missing")
	}
}

// New --device reaches the RPC seam with the stable device_id field; legacy
// --id stops in Cobra before ensure/RPC and cannot silently remain compatible.
func TestPeerCommands_should_routeDeviceFlag_and_rejectLegacyID(t *testing.T) {
	called := false
	call := func(method string, params map[string]any) (map[string]any, error) {
		called = true
		if method != "peer.add" || params["device_id"] != "123456789" {
			t.Fatal(method, params)
		}
		return map[string]any{}, nil
	}
	ensure := func() error { return nil }
	command := cmdPeerWith(ensure, call)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"add", "--device", "123456789", "--grant", "grant"})
	if err := command.Execute(); err != nil || !called {
		t.Fatal(err, called)
	}
	legacy := cmdPeerWith(func() error { t.Fatal("legacy flag reached ensure"); return nil }, func(string, map[string]any) (map[string]any, error) {
		t.Fatal("legacy flag reached RPC")
		return nil, nil
	})
	legacy.SetOut(io.Discard)
	legacy.SetErr(io.Discard)
	legacy.SetArgs([]string{"add", "--id", "123456789", "--grant", "grant"})
	err := legacy.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatal("legacy --id accepted", err)
	}
}
