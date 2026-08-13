package run

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/ciuser"
	"github.com/wodby/wodby-cli/pkg/config"
)

func TestResolveRunUser(t *testing.T) {
	t.Cleanup(func() {
		resolveBindUser = ciuser.ResolveBindUser
	})

	t.Run("uses explicit user override", func(t *testing.T) {
		got, err := resolveRunUser("root", &config.Config{})
		if err != nil || got != "root" {
			t.Fatalf("resolveRunUser() = %q, %v, want root", got, err)
		}
	})

	t.Run("keeps image user for data container", func(t *testing.T) {
		got, err := resolveRunUser("", &config.Config{DataContainer: "data"})
		if err != nil || got != "" {
			t.Fatalf("resolveRunUser() = %q, %v, want empty user", got, err)
		}
	})

	t.Run("uses current uid gid for bind mounted contexts", func(t *testing.T) {
		resolveBindUser = func(string) (string, error) { return "1001:121", nil }
		got, err := resolveRunUser("", &config.Config{})
		if err != nil || got != "1001:121" {
			t.Fatalf("resolveRunUser() = %q, %v, want 1001:121", got, err)
		}
	})

	t.Run("returns bind user lookup errors", func(t *testing.T) {
		resolveBindUser = func(string) (string, error) { return "", errors.New("boom") }
		if _, err := resolveRunUser("", &config.Config{}); err == nil {
			t.Fatal("resolveRunUser() error = nil, want error")
		}
	})
}

func TestShouldClearImageEntrypoint(t *testing.T) {
	tests := []struct {
		name               string
		explicitEntrypoint string
		user               string
		want               bool
	}{
		{name: "numeric uid gid", user: "1001:1001", want: true},
		{name: "numeric uid named group", user: "1001:www-data", want: true},
		{name: "named user", user: "wodby", want: false},
		{name: "explicit entrypoint", explicitEntrypoint: "composer", user: "1001:1001", want: false},
		{name: "no user", user: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldClearImageEntrypoint(tt.explicitEntrypoint, tt.user)
			if got != tt.want {
				t.Fatalf("shouldClearImageEntrypoint(%q, %q) = %t, want %t", tt.explicitEntrypoint, tt.user, got, tt.want)
			}
		})
	}
}
