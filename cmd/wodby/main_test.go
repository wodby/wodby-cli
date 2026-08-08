package main

import "testing"

func TestRootCommandPreservesLegacyCommandSurface(t *testing.T) {
	wantCommands := map[string]bool{
		"ci":      false,
		"version": false,
	}

	for _, command := range RootCmd.Commands() {
		if command.Name() == "completion" {
			t.Fatal("dependency upgrade exposed a new completion command")
		}
		if _, ok := wantCommands[command.Name()]; ok {
			wantCommands[command.Name()] = true
		}
	}

	for command, found := range wantCommands {
		if !found {
			t.Fatalf("legacy command %q is missing", command)
		}
	}
}

func TestRootCommandPreservesLegacyFlagDefaults(t *testing.T) {
	tests := map[string]string{
		"api-key":        "",
		"api-proto":      "https",
		"api-host":       "api.wodby.com",
		"api-prefix":     "api/v2",
		"ci-config-path": "/tmp/.wodby-ci.json",
		"verbose":        "false",
	}

	for name, want := range tests {
		flag := RootCmd.PersistentFlags().Lookup(name)
		if flag == nil {
			t.Fatalf("legacy flag %q is missing", name)
		}
		if flag.DefValue != want {
			t.Fatalf("flag %q default = %q, want %q", name, flag.DefValue, want)
		}
	}
}
