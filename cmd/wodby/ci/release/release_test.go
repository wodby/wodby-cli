package release

import (
	"reflect"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestReleaseExtraTags(t *testing.T) {
	tests := []struct {
		name         string
		gitRefType   types.GitRefType
		gitRef       string
		latestBranch string
		branchTag    bool
		want         []string
	}{
		{
			name:         "latest branch gets latest and branch tags",
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
			gitRefType:   types.GitRefTypeBranch,
			gitRef:       "feature-login",
			latestBranch: "master",
			branchTag:    true,
			want:         []string{"registry.example.com/apps/demo:feature-login"},
		},
		{
			name:         "tags do not get branch aliases",
			gitRefType:   types.GitRefTypeTag,
			gitRef:       "v2.0.0",
			latestBranch: "master",
			branchTag:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := releaseExtraTags(
				"registry.example.com/apps/demo:php-123",
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
