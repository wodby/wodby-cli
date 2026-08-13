package run

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestResolveRunUser(t *testing.T) {
	t.Cleanup(func() {
		currentHostUser = defaultCurrentHostUser
		workspaceOwner = defaultWorkspaceOwner
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

	t.Run("resolves uid 1000 for bind mounted contexts", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "1000:1000", nil
		}

		got, err := resolveRunUser("", &types.Config{})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}

		if got != "1000:1000" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "1000:1000")
		}
	})

	t.Run("uses workspace owner when cli runs as root", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "0:0", nil
		}
		workspaceOwner = func(string) (string, error) {
			return "1002:121", nil
		}

		got, err := resolveRunUser("", &types.Config{Context: "/workspace"})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "1002:121" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "1002:121")
		}
	})

	t.Run("keeps root when workspace is root owned", func(t *testing.T) {
		currentHostUser = func() (string, error) {
			return "0:0", nil
		}
		workspaceOwner = func(string) (string, error) {
			return "0:0", nil
		}

		got, err := resolveRunUser("", &types.Config{Context: "/workspace"})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "0:0" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "0:0")
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

func TestUsersForImage(t *testing.T) {
	tests := []struct {
		name          string
		workspaceUser string
		imageUID      uint32
		wantDocker    string
		wantCache     string
	}{
		{
			name:          "matching identity preserves image user",
			workspaceUser: "1000:1000",
			imageUID:      1000,
			wantCache:     "1000:1000",
		},
		{
			name:          "matching uid ignores group difference",
			workspaceUser: "1000:121",
			imageUID:      1000,
			wantCache:     "1000:121",
		},
		{
			name:          "different uid maps workspace user",
			workspaceUser: "1001:121",
			imageUID:      1000,
			wantDocker:    "1001:121",
			wantCache:     "1001:121",
		},
		{
			name:          "invalid workspace identity remains explicit",
			workspaceUser: "wodby",
			imageUID:      1000,
			wantDocker:    "wodby",
			wantCache:     "wodby",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usersForImage(tt.workspaceUser, tt.imageUID)
			if got.docker != tt.wantDocker || got.cache != tt.wantCache {
				t.Fatalf("usersForImage(%q, %d) = docker:%q cache:%q, want docker:%q cache:%q", tt.workspaceUser, tt.imageUID, got.docker, got.cache, tt.wantDocker, tt.wantCache)
			}
		})
	}
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
