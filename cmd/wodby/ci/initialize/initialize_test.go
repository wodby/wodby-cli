package initialize

import (
	"reflect"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

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
		customStack      bool
		provider         string
		want             bool
	}{
		{name: "unknown managed bind mount is unchanged", provider: "Unknown"},
		{name: "custom stack in known CI is unchanged", customStack: true, provider: types.CircleCI},
		{name: "explicit bind permission fix", explicit: true, want: true},
		{name: "data container is prepared", hasDataContainer: true, want: true},
		{name: "managed CircleCI checkout is prepared", provider: types.CircleCI, want: true},
		{name: "managed GitHub checkout is prepared", provider: types.GitHubActions, want: true},
		{name: "managed GitLab checkout is prepared", provider: types.GitLab, want: true},
		{name: "managed Bitbucket checkout is prepared", provider: types.BitbucketPipelines, want: true},
		{name: "managed Jenkins checkout is prepared", provider: types.Jenkins, want: true},
		{name: "managed Travis checkout is prepared", provider: types.TravisCI, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := permissionFixDecision(tt.explicit, tt.hasDataContainer, tt.customStack, tt.provider)
			if got != tt.want {
				t.Fatalf("permissionFixDecision() = %t (%s), want %t", got, reason, tt.want)
			}
			if reason == "" {
				t.Fatal("permissionFixDecision() returned an empty reason")
			}
		})
	}
}

func TestManagedInitRunConfigPreservesImageContract(t *testing.T) {
	tests := []struct {
		name          string
		dataContainer string
		environment   map[string]interface{}
		wantVolumes   []string
		wantFrom      []string
		wantEnv       []string
	}{
		{
			name:        "native bind mount",
			environment: map[string]interface{}{"DRUPAL_SITE": "default"},
			wantVolumes: []string{"/workspace:/var/www/html"},
			wantEnv:     []string{"DRUPAL_SITE=default"},
		},
		{
			name:          "Docker-in-Docker data volume",
			dataContainer: "wodby-data",
			environment:   map[string]interface{}{"DRUPAL_SITE": "default"},
			wantFrom:      []string{"wodby-data"},
			wantEnv:       []string{"DRUPAL_SITE=default"},
		},
		{
			name:        "explicit home is preserved",
			environment: map[string]interface{}{"HOME": "/custom-home"},
			wantVolumes: []string{"/workspace:/var/www/html"},
			wantEnv:     []string{"HOME=/custom-home"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := managedInitRunConfig(
				"wodby/drupal-php:8.3",
				"/var/www/html",
				"/workspace",
				tt.dataContainer,
				tt.environment,
			)
			if config.User != "" {
				t.Fatalf("managed initializer user = %q, want image default", config.User)
			}
			if config.ClearEntrypoint || config.Entrypoint != "" {
				t.Fatalf("managed initializer entrypoint override = clear:%t value:%q", config.ClearEntrypoint, config.Entrypoint)
			}
			if !reflect.DeepEqual(config.Volumes, tt.wantVolumes) {
				t.Fatalf("managed initializer volumes = %#v, want %#v", config.Volumes, tt.wantVolumes)
			}
			if !reflect.DeepEqual(config.VolumesFrom, tt.wantFrom) {
				t.Fatalf("managed initializer volumes-from = %#v, want %#v", config.VolumesFrom, tt.wantFrom)
			}
			if !reflect.DeepEqual(config.Env, tt.wantEnv) {
				t.Fatalf("managed initializer env = %#v, want %#v", config.Env, tt.wantEnv)
			}
		})
	}
}
