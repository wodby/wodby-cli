package build

import (
	"path/filepath"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestDataContainerContextPath(t *testing.T) {
	got := dataContainerContextPath("test-container")
	want := "/tmp/wodby-build-test-container"

	if got != want {
		t.Fatalf("dataContainerContextPath() = %q, want %q", got, want)
	}
}

func TestNewBuildFiles(t *testing.T) {
	t.Run("service dockerfile path", func(t *testing.T) {
		got := newBuildFiles("contexts/app", "php", "")

		if got.dockerfileName != "php_Dockerfile" {
			t.Fatalf("dockerfileName = %q, want %q", got.dockerfileName, "php_Dockerfile")
		}
		if got.dockerfilePath != filepath.Join("contexts/app", "php_Dockerfile") {
			t.Fatalf("dockerfilePath = %q, want %q", got.dockerfilePath, filepath.Join("contexts/app", "php_Dockerfile"))
		}
		if got.dockerignoreName != "php_Dockerfile.dockerignore" {
			t.Fatalf("dockerignoreName = %q, want %q", got.dockerignoreName, "php_Dockerfile.dockerignore")
		}
		if got.dockerignorePath != filepath.Join("contexts/app", "php_Dockerfile.dockerignore") {
			t.Fatalf("dockerignorePath = %q, want %q", got.dockerignorePath, filepath.Join("contexts/app", "php_Dockerfile.dockerignore"))
		}
	})

	t.Run("explicit dockerfile path", func(t *testing.T) {
		got := newBuildFiles("contexts/app", "php", "docker/nginx.Dockerfile")

		if got.dockerfileName != "nginx.Dockerfile" {
			t.Fatalf("dockerfileName = %q, want %q", got.dockerfileName, "nginx.Dockerfile")
		}
		if got.dockerfilePath != filepath.Join("contexts/app", "docker/nginx.Dockerfile") {
			t.Fatalf("dockerfilePath = %q, want %q", got.dockerfilePath, filepath.Join("contexts/app", "docker/nginx.Dockerfile"))
		}
		if got.dockerignoreName != "nginx.Dockerfile.dockerignore" {
			t.Fatalf("dockerignoreName = %q, want %q", got.dockerignoreName, "nginx.Dockerfile.dockerignore")
		}
		if got.dockerignorePath != filepath.Join("contexts/app", "docker/nginx.Dockerfile.dockerignore") {
			t.Fatalf("dockerignorePath = %q, want %q", got.dockerignorePath, filepath.Join("contexts/app", "docker/nginx.Dockerfile.dockerignore"))
		}
	})
}

func TestResolveCacheOptions(t *testing.T) {
	config := &types.Config{
		DataContainer: "data-container",
		AppBuild: types.AppBuild{
			Config: &types.AppBuildConfig{
				RegistryHost:       "registry.example.com",
				RegistryRepository: "apps/demo",
			},
		},
	}

	t.Run("uses registry cache for dind by default", func(t *testing.T) {
		gotFrom, gotTo, err := resolveCacheOptions(config, "php", options{cacheBackend: "auto"})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}

		wantFrom := []string{"type=registry,ref=registry.example.com/apps/demo:php-buildcache"}
		wantTo := []string{"type=registry,ref=registry.example.com/apps/demo:php-buildcache,mode=max"}

		if len(gotFrom) != len(wantFrom) || gotFrom[0] != wantFrom[0] {
			t.Fatalf("cacheFrom = %v, want %v", gotFrom, wantFrom)
		}
		if len(gotTo) != len(wantTo) || gotTo[0] != wantTo[0] {
			t.Fatalf("cacheTo = %v, want %v", gotTo, wantTo)
		}
	})

	t.Run("keeps explicit cache settings", func(t *testing.T) {
		cacheFrom := []string{"type=registry,ref=custom/from"}
		cacheTo := []string{"type=registry,ref=custom/to"}

		gotFrom, gotTo, err := resolveCacheOptions(config, "php", options{
			cacheBackend: "local",
			cacheDir:     ".buildx-cache",
			cacheFrom:    cacheFrom,
			cacheTo:      cacheTo,
		})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}

		if len(gotFrom) != 1 || gotFrom[0] != cacheFrom[0] {
			t.Fatalf("cacheFrom = %v, want %v", gotFrom, cacheFrom)
		}
		if len(gotTo) != 1 || gotTo[0] != cacheTo[0] {
			t.Fatalf("cacheTo = %v, want %v", gotTo, cacheTo)
		}
	})

	t.Run("does not inject registry cache outside dind", func(t *testing.T) {
		plainConfig := &types.Config{
			AppBuild: config.AppBuild,
		}

		gotFrom, gotTo, err := resolveCacheOptions(plainConfig, "php", options{cacheBackend: "auto"})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}

		if gotFrom != nil {
			t.Fatalf("cacheFrom = %v, want nil", gotFrom)
		}
		if gotTo != nil {
			t.Fatalf("cacheTo = %v, want nil", gotTo)
		}
	})

	t.Run("uses local cache when cache dir is set", func(t *testing.T) {
		plainConfig := &types.Config{
			AppBuild: config.AppBuild,
		}

		gotFrom, gotTo, err := resolveCacheOptions(plainConfig, "php", options{cacheDir: ".buildx-cache"})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}

		wantFrom := []string{"type=local,src=.buildx-cache"}
		wantTo := []string{"type=local,dest=.buildx-cache,mode=max"}

		if len(gotFrom) != len(wantFrom) || gotFrom[0] != wantFrom[0] {
			t.Fatalf("cacheFrom = %v, want %v", gotFrom, wantFrom)
		}
		if len(gotTo) != len(wantTo) || gotTo[0] != wantTo[0] {
			t.Fatalf("cacheTo = %v, want %v", gotTo, wantTo)
		}
	})

	t.Run("uses explicit registry backend and ref", func(t *testing.T) {
		plainConfig := &types.Config{
			AppBuild: config.AppBuild,
		}

		gotFrom, gotTo, err := resolveCacheOptions(plainConfig, "php", options{
			cacheBackend: "registry",
			cacheRef:     "registry.example.com/custom/cache:php",
			cacheMode:    "min",
		})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}

		wantFrom := []string{"type=registry,ref=registry.example.com/custom/cache:php"}
		wantTo := []string{"type=registry,ref=registry.example.com/custom/cache:php,mode=min"}

		if len(gotFrom) != len(wantFrom) || gotFrom[0] != wantFrom[0] {
			t.Fatalf("cacheFrom = %v, want %v", gotFrom, wantFrom)
		}
		if len(gotTo) != len(wantTo) || gotTo[0] != wantTo[0] {
			t.Fatalf("cacheTo = %v, want %v", gotTo, wantTo)
		}
	})

	t.Run("supports none backend", func(t *testing.T) {
		gotFrom, gotTo, err := resolveCacheOptions(config, "php", options{cacheBackend: "none"})
		if err != nil {
			t.Fatalf("resolveCacheOptions() error = %v", err)
		}
		if gotFrom != nil {
			t.Fatalf("cacheFrom = %v, want nil", gotFrom)
		}
		if gotTo != nil {
			t.Fatalf("cacheTo = %v, want nil", gotTo)
		}
	})

	t.Run("rejects unknown backend", func(t *testing.T) {
		_, _, err := resolveCacheOptions(config, "php", options{cacheBackend: "weird"})
		if err == nil {
			t.Fatal("resolveCacheOptions() error = nil, want error")
		}
	})
}
