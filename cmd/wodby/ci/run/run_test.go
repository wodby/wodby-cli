package run

import (
	"testing"

	"github.com/wodby/wodby-cli/pkg/config"
)

func TestResolveRunUser(t *testing.T) {
	t.Run("uses explicit user", func(t *testing.T) {
		got, err := resolveRunUser("1001:1002", &config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if got != "1001:1002" {
			t.Fatalf("resolveRunUser() = %q, want explicit identity", got)
		}
	})

	t.Run("keeps image user for data container", func(t *testing.T) {
		got, err := resolveRunUser("", &config.Config{DataContainer: "wodby-data"})
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("resolveRunUser() = %q, want image default", got)
		}
	})

	t.Run("uses native host identity", func(t *testing.T) {
		got, err := resolveRunUser("", &config.Config{Context: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if got == "" {
			t.Fatal("resolveRunUser() returned an empty native identity")
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
			name:          "named workspace user cannot match numeric image uid",
			workspaceUser: "runner:runner",
			imageUID:      1000,
			wantDocker:    "runner:runner",
			wantCache:     "runner:runner",
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
