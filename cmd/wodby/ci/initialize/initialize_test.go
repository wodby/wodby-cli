package initialize

import "testing"

func TestProviderFlagHasShorthandAndNoDefault(t *testing.T) {
	flag := Cmd.Flags().Lookup("provider")
	if flag == nil {
		t.Fatal("provider flag was not registered")
	}
	if flag.Shorthand != "p" {
		t.Fatalf("provider shorthand = %q, want %q", flag.Shorthand, "p")
	}
	if flag.DefValue != "" {
		t.Fatalf("provider default = %q, want empty string", flag.DefValue)
	}
}

func TestPermissionFixDecision(t *testing.T) {
	tests := []struct {
		name             string
		explicit         bool
		hasDataContainer bool
		want             bool
	}{
		{name: "bind mount is unchanged by default"},
		{name: "explicit bind permission fix", explicit: true, want: true},
		{name: "data container is prepared", hasDataContainer: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := permissionFixDecision(tt.explicit, tt.hasDataContainer)
			if got != tt.want {
				t.Fatalf("permissionFixDecision() = %t (%s), want %t", got, reason, tt.want)
			}
			if reason == "" {
				t.Fatal("permissionFixDecision() returned an empty reason")
			}
		})
	}
}

func TestShouldMapInitUser(t *testing.T) {
	if !shouldMapInitUser(false, false) {
		t.Fatal("bind-mounted initializer should use the workspace identity")
	}
	if shouldMapInitUser(true, false) {
		t.Fatal("explicit permission fix should preserve the image identity")
	}
	if shouldMapInitUser(false, true) {
		t.Fatal("DinD initializer should preserve the image identity")
	}
}
