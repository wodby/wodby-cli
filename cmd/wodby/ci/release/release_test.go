package release

import (
	"reflect"
	"strings"
	"testing"

	"github.com/distribution/reference"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestReleaseExtraTags(t *testing.T) {
	tests := []struct {
		name         string
		image        string
		gitRefType   types.GitRefType
		gitRef       string
		latestBranch string
		branchTag    bool
		want         []string
	}{
		{
			name:         "latest branch gets latest and branch tags",
			image:        "registry.example.com/apps/demo:php-123",
			gitRefType:   types.GitRefTypeBranch,
			gitRef:       "master",
			latestBranch: "master",
			branchTag:    true,
			want: []string{
				"registry.example.com/apps/demo:latest",
				"registry.example.com/apps/demo:master",
			},
		},
		{
			name:         "branch tag is optional",
			image:        "registry.example.com/apps/demo:php-123",
			gitRefType:   types.GitRefTypeBranch,
			gitRef:       "feature-login",
			latestBranch: "master",
			branchTag:    true,
			want:         []string{"registry.example.com/apps/demo:feature-login"},
		},
		{
			name:         "tags do not get branch aliases",
			image:        "registry.example.com/apps/demo:php-123",
			gitRefType:   types.GitRefTypeTag,
			gitRef:       "v2.0.0",
			latestBranch: "master",
			branchTag:    true,
		},
		{
			name:         "registry port and repository are preserved",
			image:        "registry.example.com:5000/team/apps/demo:php-123",
			gitRefType:   types.GitRefTypeBranch,
			gitRef:       "main",
			latestBranch: "main",
			branchTag:    true,
			want: []string{
				"registry.example.com:5000/team/apps/demo:latest",
				"registry.example.com:5000/team/apps/demo:main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := releaseExtraTags(
				tt.image,
				tt.gitRefType,
				tt.gitRef,
				tt.latestBranch,
				tt.branchTag,
			)
			if err != nil {
				t.Fatalf("releaseExtraTags() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("releaseExtraTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDockerTagFromBranch(t *testing.T) {
	validReference, err := reference.WithName("registry.example.com/apps/demo")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		branch    string
		unchanged bool
		prefix    string
	}{
		{name: "simple branch stays unchanged", branch: "feature-login", unchanged: true},
		{name: "slash is replaced", branch: "feature/login", prefix: "feature-login-"},
		{name: "unicode is replaced", branch: "feature/авторизация", prefix: "feature-"},
		{name: "invalid leading characters are removed", branch: "../feature", prefix: "feature-"},
		{name: "long branch is truncated", branch: strings.Repeat("a", 200), prefix: strings.Repeat("a", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dockerTagFromBranch(tt.branch)
			if err != nil {
				t.Fatalf("dockerTagFromBranch() error = %v", err)
			}
			if tt.unchanged && got != tt.branch {
				t.Fatalf("dockerTagFromBranch() = %q, want unchanged branch %q", got, tt.branch)
			}
			if tt.prefix != "" && !strings.HasPrefix(got, tt.prefix) {
				t.Fatalf("dockerTagFromBranch() = %q, want prefix %q", got, tt.prefix)
			}
			if len(got) > dockerTagMaxLength {
				t.Fatalf("dockerTagFromBranch() length = %d, want at most %d", len(got), dockerTagMaxLength)
			}
			if _, err := reference.WithTag(validReference, got); err != nil {
				t.Fatalf("dockerTagFromBranch() produced invalid tag %q: %v", got, err)
			}
			gotAgain, err := dockerTagFromBranch(tt.branch)
			if err != nil {
				t.Fatal(err)
			}
			if gotAgain != got {
				t.Fatalf("dockerTagFromBranch() is not deterministic: %q then %q", got, gotAgain)
			}
		})
	}
}

func TestDockerTagFromBranchAvoidsSanitizationCollisions(t *testing.T) {
	slashTag, err := dockerTagFromBranch("feature/login")
	if err != nil {
		t.Fatal(err)
	}
	atTag, err := dockerTagFromBranch("feature@login")
	if err != nil {
		t.Fatal(err)
	}
	plainTag, err := dockerTagFromBranch("feature-login")
	if err != nil {
		t.Fatal(err)
	}

	if slashTag == atTag || slashTag == plainTag || atTag == plainTag {
		t.Fatalf("sanitized tags must remain distinct: slash=%q at=%q plain=%q", slashTag, atTag, plainTag)
	}
}

func TestDockerTagFromBranchRejectsEmptyBranch(t *testing.T) {
	if _, err := dockerTagFromBranch(""); err == nil {
		t.Fatal("dockerTagFromBranch() error = nil, want empty branch error")
	}
}
