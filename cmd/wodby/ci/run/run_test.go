package run

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestResolveRunUser(t *testing.T) {
	t.Cleanup(func() {
		currentHostUser = defaultCurrentHostUser
	})

	t.Run("uses explicit user override", func(t *testing.T) {
		got, err := resolveRunUser("root", &types.Config{})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "root" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "root")
		}
	})

	t.Run("skips host user mapping for data container", func(t *testing.T) {
		got, err := resolveRunUser("", &types.Config{DataContainer: "data"})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "" {
			t.Fatalf("resolveRunUser() = %q, want empty user", got)
		}
	})

	t.Run("skips custom user for default image uid gid", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "1000:1000", nil
		}

		got, err := resolveRunUser("", &types.Config{})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}

		if got != "" {
			t.Fatalf("resolveRunUser() = %q, want empty user", got)
		}
	})

	t.Run("uses current user when it differs from image uid gid", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "1001:121", nil
		}

		got, err := resolveRunUser("", &types.Config{})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "1001:121" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "1001:121")
		}
	})

	t.Run("returns current user lookup errors", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "", errors.New("boom")
		}

		_, err := resolveRunUser("", &types.Config{})
		if err == nil {
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
		{
			name: "clears entrypoint for numeric uid gid user",
			user: "1001:1001",
			want: true,
		},
		{
			name: "clears entrypoint for numeric uid with named group",
			user: "1001:www-data",
			want: true,
		},
		{
			name: "keeps entrypoint for named user",
			user: "wodby",
			want: false,
		},
		{
			name:               "keeps entrypoint when explicit entrypoint provided",
			explicitEntrypoint: "composer",
			user:               "1001:1001",
			want:               false,
		},
		{
			name: "keeps entrypoint when no user is set",
			user: "",
			want: false,
		},
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
