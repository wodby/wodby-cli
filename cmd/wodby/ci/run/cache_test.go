package run

import (
	"errors"
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

func TestResolveHostCacheStorage(t *testing.T) {
	t.Run("uses conventional home paths for native runs", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("WODBY_CI_CACHE_DIR", "")

		gotHome, gotRoot, err := resolveHostCacheStorage(t.TempDir(), false)
		if err != nil {
			t.Fatal(err)
		}
		if gotHome != home || gotRoot != "" {
			t.Fatalf("resolveHostCacheStorage() = %q, %q, want %q, empty root", gotHome, gotRoot, home)
		}
	})

	t.Run("uses project staging root for data containers", func(t *testing.T) {
		context := t.TempDir()
		t.Setenv("WODBY_CI_CACHE_DIR", "")

		gotHome, gotRoot, err := resolveHostCacheStorage(context, true)
		if err != nil {
			t.Fatal(err)
		}
		wantRoot := filepath.Join(context, ".wodby-ci-cache")
		if gotHome != "" || gotRoot != wantRoot {
			t.Fatalf("resolveHostCacheStorage() = %q, %q, want empty home, %q", gotHome, gotRoot, wantRoot)
		}
	})

	t.Run("explicit root overrides native home paths", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("WODBY_CI_CACHE_DIR", root)

		gotHome, gotRoot, err := resolveHostCacheStorage(t.TempDir(), false)
		if err != nil {
			t.Fatal(err)
		}
		if gotHome != "" || gotRoot != root {
			t.Fatalf("resolveHostCacheStorage() = %q, %q, want empty home, %q", gotHome, gotRoot, root)
		}
	})
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

func TestCacheConfigurationIsExplicit(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   bool
	}{
		{name: "implicit"},
		{name: "auto", values: []string{"auto"}},
		{name: "none", values: []string{"none"}},
		{name: "profile", values: []string{"composer"}, want: true},
		{name: "profiles", values: []string{"composer,npm"}, want: true},
		{name: "invalid explicit combination", values: []string{"auto,composer"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheConfigurationIsExplicit(tt.values); got != tt.want {
				t.Fatalf("cacheConfigurationIsExplicit(%v) = %t, want %t", tt.values, got, tt.want)
			}
		})
	}
}

func TestHandleCacheFailure(t *testing.T) {
	sentinel := errors.New("cache unavailable")
	if err := handleCacheFailure("setup", false, sentinel); err != nil {
		t.Fatalf("automatic cache failure returned %v", err)
	}
	if err := handleCacheFailure("setup", true, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("explicit cache failure = %v, want sentinel", err)
	}
}

func TestAddCacheProfiles(t *testing.T) {
	hostHome := t.TempDir()
	config := docker.RunConfig{}
	active, err := addCacheProfiles(&config, []string{"npm", "composer", "bundler", "uv"}, map[string]struct{}{}, hostHome, "", false, "1001:1001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"npm", "composer", "bundler", "uv"}; !reflect.DeepEqual(active, want) {
		t.Fatalf("active caches = %#v, want %#v", active, want)
	}
	wantVolumes := []string{
		filepath.Join(hostHome, ".npm") + ":/tmp/wodby-cache/npm",
		filepath.Join(hostHome, ".composer", "cache") + ":/tmp/wodby-cache/composer",
		filepath.Join(hostHome, ".bundle", "cache") + ":/tmp/wodby-cache/bundler",
		filepath.Join(hostHome, ".cache", "uv") + ":/tmp/wodby-cache/uv",
	}
	if !reflect.DeepEqual(config.Volumes, wantVolumes) {
		t.Fatalf("cache volumes = %#v, want %#v", config.Volumes, wantVolumes)
	}
}

func TestAddCacheProfilesUsesExplicitRoot(t *testing.T) {
	cacheRoot := t.TempDir()
	config := docker.RunConfig{}
	active, err := addCacheProfiles(&config, []string{"npm"}, map[string]struct{}{}, "", cacheRoot, false, "1001:1001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"npm"}; !reflect.DeepEqual(active, want) {
		t.Fatalf("active caches = %#v, want %#v", active, want)
	}
	wantVolumes := []string{filepath.Join(cacheRoot, "npm") + ":/tmp/wodby-cache/npm"}
	if !reflect.DeepEqual(config.Volumes, wantVolumes) {
		t.Fatalf("cache volumes = %#v, want %#v", config.Volumes, wantVolumes)
	}
}

func TestAddCacheProfilesPreservesExplicitConfiguration(t *testing.T) {
	hostHome := t.TempDir()
	config := docker.RunConfig{Volumes: []string{"custom-cache:/tmp/wodby-cache/composer"}, Env: []string{"CI=true"}}
	explicitEnv := map[string]struct{}{"NPM_CONFIG_CACHE": {}, "BUNDLE_USER_CACHE": {}}
	active, err := addCacheProfiles(&config, []string{"npm", "composer", "bundler"}, explicitEnv, hostHome, "", false, "1001:1001")
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
	active, err := addCacheProfiles(&config, []string{"npm"}, map[string]struct{}{}, "", filepath.Join(t.TempDir(), "unused"), true, "")
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

func TestNativeCacheOwnershipRunUsesImageAsRoot(t *testing.T) {
	home := t.TempDir()
	config, args := nativeCacheOwnershipRun(
		"wodby/drupal-php:8.3-4.82.4",
		[]string{"composer"},
		home,
		"",
		"1000:1000",
	)

	if config.Image != "wodby/drupal-php:8.3-4.82.4" || config.User != "root" || !config.ClearEntrypoint {
		t.Fatalf("native cache ownership config = %#v", config)
	}
	wantVolumes := []string{filepath.Join(home, ".composer", "cache") + ":/tmp/wodby-cache/composer"}
	if !reflect.DeepEqual(config.Volumes, wantVolumes) {
		t.Fatalf("native cache ownership volumes = %#v, want %#v", config.Volumes, wantVolumes)
	}
	wantArgs := []string{"chown", "-R", "1000:1000", "/tmp/wodby-cache/composer"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("native cache ownership args = %#v, want %#v", args, wantArgs)
	}
}
