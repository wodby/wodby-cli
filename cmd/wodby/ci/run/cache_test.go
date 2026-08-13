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
	t.Run("adds writable fallback", func(t *testing.T) {
		got := withMappedUserHome([]string{"CI=true"}, map[string]struct{}{})
		want := []string{"CI=true", "HOME=/tmp"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("withMappedUserHome() = %#v, want %#v", got, want)
		}
	})

	t.Run("preserves explicit home", func(t *testing.T) {
		env := []string{"HOME=/workspace/home"}
		got := withMappedUserHome(env, map[string]struct{}{"HOME": {}})
		if !reflect.DeepEqual(got, env) {
			t.Fatalf("withMappedUserHome() = %#v, want %#v", got, env)
		}
	})
}

func TestResolveRunCacheProfileNames(t *testing.T) {
	t.Run("disables automatic caches for explicit user", func(t *testing.T) {
		got, err := resolveRunCacheProfileNames(nil, false, "explicit", "wodby/node:24", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("resolveRunCacheProfileNames() = %#v, want nil", got)
		}
	})

	t.Run("allows explicit cache when automatic detection is disabled", func(t *testing.T) {
		got, err := resolveRunCacheProfileNames([]string{"npm"}, false, "explicit", "wodby/node:24", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"npm"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resolveRunCacheProfileNames() = %#v, want %#v", got, want)
		}
	})

	t.Run("keeps automatic caches for bind mounted context", func(t *testing.T) {
		got, err := resolveRunCacheProfileNames(nil, false, "", "wodby/node:24", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"npm"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resolveRunCacheProfileNames() = %#v, want %#v", got, want)
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
		{
			name:        "uses image label",
			autoAllowed: true,
			image:       "registry.example.com/app:latest",
			labels:      map[string]string{cacheLabel: "npm, composer, bundler"},
			want:        []string{"npm", "composer", "bundler"},
		},
		{
			name:        "empty image label disables fallback",
			autoAllowed: true,
			image:       "wodby/node:24",
			labels:      map[string]string{cacheLabel: ""},
		},
		{
			name:        "recognizes older wodby node image",
			autoAllowed: true,
			image:       "wodby/node:24",
			want:        []string{"npm"},
		},
		{
			name:        "recognizes official node image",
			autoAllowed: true,
			image:       "node:24-alpine",
			want:        []string{"npm"},
		},
		{
			name:        "recognizes official composer image",
			autoAllowed: true,
			image:       "composer:2",
			want:        []string{"composer"},
		},
		{
			name:        "recognizes older wodby ruby image",
			autoAllowed: true,
			image:       "wodby/ruby:4",
			want:        []string{"bundler"},
		},
		{
			name:        "recognizes official ruby image",
			autoAllowed: true,
			image:       "ruby:4-alpine",
			want:        []string{"bundler"},
		},
		{
			name:        "explicit profiles override detection",
			explicit:    []string{"uv,npm"},
			autoAllowed: true,
			image:       "wodby/php:8.4",
			want:        []string{"uv", "npm"},
		},
		{
			name:        "auto can be requested explicitly",
			explicit:    []string{"auto"},
			autoAllowed: true,
			image:       "wodby/python:3.13",
			want:        []string{"uv"},
		},
		{
			name:        "none disables profiles",
			explicit:    []string{"none"},
			autoAllowed: true,
			image:       "wodby/node:24",
		},
		{
			name:        "no cache disables profiles",
			disabled:    true,
			autoAllowed: true,
			image:       "wodby/node:24",
		},
		{
			name:        "auto detection can be disabled for explicit users",
			autoAllowed: false,
			image:       "wodby/node:24",
		},
		{
			name:        "explicit profile still works when auto is unavailable",
			explicit:    []string{"npm"},
			autoAllowed: false,
			image:       "custom/image:latest",
			want:        []string{"npm"},
		},
		{
			name:        "rejects unknown explicit profile",
			explicit:    []string{"pnpm"},
			autoAllowed: true,
			wantErr:     true,
		},
		{
			name:     "rejects conflicting cache flags",
			explicit: []string{"npm"},
			disabled: true,
			wantErr:  true,
		},
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

	wantEnv := []string{
		"NPM_CONFIG_CACHE=/tmp/wodby-cache/npm",
		"COMPOSER_CACHE_DIR=/tmp/wodby-cache/composer",
		"BUNDLE_USER_CACHE=/tmp/wodby-cache/bundler",
		"UV_CACHE_DIR=/tmp/wodby-cache/uv",
	}
	if !reflect.DeepEqual(config.Env, wantEnv) {
		t.Fatalf("cache env = %#v, want %#v", config.Env, wantEnv)
	}

	wantVolumes := []string{
		filepath.Join(cacheRoot, "npm") + ":/tmp/wodby-cache/npm",
		filepath.Join(cacheRoot, "composer") + ":/tmp/wodby-cache/composer",
		filepath.Join(cacheRoot, "bundler") + ":/tmp/wodby-cache/bundler",
		filepath.Join(cacheRoot, "uv") + ":/tmp/wodby-cache/uv",
	}
	if !reflect.DeepEqual(config.Volumes, wantVolumes) {
		t.Fatalf("cache volumes = %#v, want %#v", config.Volumes, wantVolumes)
	}

	for _, path := range []string{
		filepath.Join(cacheRoot, "npm"),
		filepath.Join(cacheRoot, "composer"),
		filepath.Join(cacheRoot, "bundler"),
		filepath.Join(cacheRoot, "uv"),
	} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("cache path %q was not created as a directory", path)
		}
	}
}

func TestAddCacheProfilesPreservesExplicitConfiguration(t *testing.T) {
	cacheRoot := t.TempDir()
	config := docker.RunConfig{
		Volumes: []string{"custom-cache:/tmp/wodby-cache/composer"},
		Env:     []string{"CI=true"},
	}
	explicitEnv := map[string]struct{}{
		"NPM_CONFIG_CACHE":  {},
		"BUNDLE_USER_CACHE": {},
	}

	active, err := addCacheProfiles(&config, []string{"npm", "composer", "bundler"}, explicitEnv, cacheRoot, false, "1001:1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active caches = %#v, want none because remaining cache uses an explicit volume", active)
	}

	wantEnv := []string{"CI=true", "COMPOSER_CACHE_DIR=/tmp/wodby-cache/composer"}
	if !reflect.DeepEqual(config.Env, wantEnv) {
		t.Fatalf("cache env = %#v, want %#v", config.Env, wantEnv)
	}
	wantVolumes := []string{"custom-cache:/tmp/wodby-cache/composer"}
	if !reflect.DeepEqual(config.Volumes, wantVolumes) {
		t.Fatalf("cache volumes = %#v, want %#v", config.Volumes, wantVolumes)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "npm")); !os.IsNotExist(err) {
		t.Fatalf("explicit npm cache unexpectedly created a host directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "bundler")); !os.IsNotExist(err) {
		t.Fatalf("explicit Bundler cache unexpectedly created a host directory: %v", err)
	}
}

func TestAddCacheProfilesUsesDataContainerVolume(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "unused")
	config := docker.RunConfig{}

	active, err := addCacheProfiles(&config, []string{"npm"}, map[string]struct{}{}, cacheRoot, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"npm"}; !reflect.DeepEqual(active, want) {
		t.Fatalf("active caches = %#v, want %#v", active, want)
	}
	if len(config.Volumes) != 0 {
		t.Fatalf("cache volumes = %#v, want none for data-container cache volume", config.Volumes)
	}
	if want := []string{"NPM_CONFIG_CACHE=/tmp/wodby-cache/npm"}; !reflect.DeepEqual(config.Env, want) {
		t.Fatalf("cache env = %#v, want %#v", config.Env, want)
	}
}
