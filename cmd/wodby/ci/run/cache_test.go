package run

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wodby/wodby-cli/pkg/docker"
)

func TestExplicitEnvironmentNames(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "run.env")
	if err := os.WriteFile(envFile, []byte("# comment\nHOME=/custom-home\nBUNDLE_USER_CACHE=/custom-bundler\nUV_CACHE_DIR=/custom-uv\n"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := explicitEnvironmentNames([]string{"CI", "NPM_CONFIG_CACHE=/custom-npm"}, envFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CI", "HOME", "NPM_CONFIG_CACHE", "BUNDLE_USER_CACHE", "UV_CACHE_DIR"} {
		if _, ok := got[name]; !ok {
			t.Errorf("explicitEnvironmentNames() missing %q", name)
		}
	}
}

func TestWithMappedUserHome(t *testing.T) {
	got := withMappedUserHome([]string{"CI=true"}, map[string]struct{}{})
	if want := []string{"CI=true", "HOME=/tmp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("withMappedUserHome() = %#v, want %#v", got, want)
	}

	env := []string{"HOME=/workspace/home"}
	got = withMappedUserHome(env, map[string]struct{}{"HOME": {}})
	if !reflect.DeepEqual(got, env) {
		t.Fatalf("withMappedUserHome() = %#v, want %#v", got, env)
	}
}

func TestResolveCacheProfileNames(t *testing.T) {
	tests := []struct {
		name        string
		explicit    []string
		disabled    bool
		autoAllowed bool
		image       string
		labels      map[string]string
		want        []string
		wantErr     bool
	}{
		{name: "uses image label", autoAllowed: true, image: "registry.example.com/app:latest", labels: map[string]string{cacheLabel: "npm, composer, bundler"}, want: []string{"npm", "composer", "bundler"}},
		{name: "empty label disables fallback", autoAllowed: true, image: "wodby/node:24", labels: map[string]string{cacheLabel: ""}},
		{name: "recognizes wodby node", autoAllowed: true, image: "wodby/node:24", want: []string{"npm"}},
		{name: "recognizes official composer", autoAllowed: true, image: "composer:2", want: []string{"composer"}},
		{name: "recognizes Wodby 1 Drupal PHP", autoAllowed: true, image: "wodby/drupal-php:8.3-4.82.4", want: []string{"composer"}},
		{name: "recognizes Wodby 1 Drupal Node", autoAllowed: true, image: "wodby/drupal-node:1.0.0", want: []string{"npm"}},
		{name: "recognizes wodby ruby", autoAllowed: true, image: "wodby/ruby:4", want: []string{"bundler"}},
		{name: "recognizes wodby python", autoAllowed: true, image: "wodby/python:3.13", want: []string{"uv"}},
		{name: "explicit profiles override detection", explicit: []string{"uv,npm"}, autoAllowed: true, image: "wodby/php:8.4", want: []string{"uv", "npm"}},
		{name: "none disables profiles", explicit: []string{"none"}, autoAllowed: true, image: "wodby/node:24"},
		{name: "explicit user disables auto", autoAllowed: false, image: "wodby/node:24"},
		{name: "explicit cache works with explicit user", explicit: []string{"npm"}, autoAllowed: false, want: []string{"npm"}},
		{name: "rejects unknown profile", explicit: []string{"pnpm"}, autoAllowed: true, wantErr: true},
		{name: "rejects conflicting flags", explicit: []string{"npm"}, disabled: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCacheProfileNames(tt.explicit, tt.disabled, tt.autoAllowed, tt.image, tt.labels)
			if tt.wantErr {
				if err == nil {
					t.Fatal("resolveCacheProfileNames() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("resolveCacheProfileNames() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAddCacheProfiles(t *testing.T) {
	cacheRoot := t.TempDir()
	config := docker.RunConfig{}
	active, err := addCacheProfiles(&config, []string{"npm", "composer", "bundler", "uv"}, map[string]struct{}{}, cacheRoot, false, "1001:1001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"npm", "composer", "bundler", "uv"}; !reflect.DeepEqual(active, want) {
		t.Fatalf("active caches = %#v, want %#v", active, want)
	}
	if len(config.Volumes) != 4 || len(config.Env) != 4 {
		t.Fatalf("cache config = %#v", config)
	}
}

func TestAddCacheProfilesPreservesExplicitConfiguration(t *testing.T) {
	cacheRoot := t.TempDir()
	config := docker.RunConfig{Volumes: []string{"custom-cache:/tmp/wodby-cache/composer"}, Env: []string{"CI=true"}}
	explicitEnv := map[string]struct{}{"NPM_CONFIG_CACHE": {}, "BUNDLE_USER_CACHE": {}}
	active, err := addCacheProfiles(&config, []string{"npm", "composer", "bundler"}, explicitEnv, cacheRoot, false, "1001:1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active caches = %#v, want none", active)
	}
	if want := []string{"CI=true", "COMPOSER_CACHE_DIR=/tmp/wodby-cache/composer"}; !reflect.DeepEqual(config.Env, want) {
		t.Fatalf("cache env = %#v, want %#v", config.Env, want)
	}
}

func TestAddCacheProfilesUsesDataContainerVolume(t *testing.T) {
	config := docker.RunConfig{}
	active, err := addCacheProfiles(&config, []string{"npm"}, map[string]struct{}{}, filepath.Join(t.TempDir(), "unused"), true, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"npm"}; !reflect.DeepEqual(active, want) {
		t.Fatalf("active caches = %#v, want %#v", active, want)
	}
	if len(config.Volumes) != 0 {
		t.Fatalf("cache volumes = %#v, want none", config.Volumes)
	}
}
