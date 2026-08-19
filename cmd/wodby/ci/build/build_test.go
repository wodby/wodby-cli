package build

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestDataContainerContextPath(t *testing.T) {
	got := dataContainerContextPath("test-container")
	want := "/tmp/wodby-build-test-container"

	if got != want {
		t.Fatalf("dataContainerContextPath() = %q, want %q", got, want)
	}
}

func TestDataContainerWorkingDirContents(t *testing.T) {
	tests := []struct {
		workingDir string
		want       string
	}{
		{workingDir: "", want: "/."},
		{workingDir: "/", want: "/."},
		{workingDir: "/var/www/html", want: "/var/www/html/."},
		{workingDir: "/var/www/html/", want: "/var/www/html/."},
	}

	for _, tt := range tests {
		t.Run(tt.workingDir, func(t *testing.T) {
			if got := dataContainerWorkingDirContents(tt.workingDir); got != tt.want {
				t.Fatalf("dataContainerWorkingDirContents(%q) = %q, want %q", tt.workingDir, got, tt.want)
			}
		})
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

func TestEnsureTemporaryDockerignore(t *testing.T) {
	context := t.TempDir()
	files := newBuildFiles(context, "php", "")

	cleanup, err := ensureTemporaryDockerignore(files, ".git")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(files.dockerignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !dockerignoreContains(string(content), ".wodby-ci-cache") {
		t.Fatalf("temporary dockerignore does not exclude cache: %q", content)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(files.dockerignorePath); !os.IsNotExist(err) {
		t.Fatalf("temporary dockerignore still exists: %v", err)
	}
}

func TestEnsureTemporaryDockerignoreRestoresExistingFile(t *testing.T) {
	context := t.TempDir()
	files := newBuildFiles(context, "php", "")
	original := []byte("vendor\n")
	if err := os.WriteFile(files.dockerignorePath, original, 0644); err != nil {
		t.Fatal(err)
	}

	cleanup, err := ensureTemporaryDockerignore(files, ".git")
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(files.dockerignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("restored dockerignore = %q, want %q", content, original)
	}
}

func TestPrioritizeMainService(t *testing.T) {
	t.Run("moves main service to the front", func(t *testing.T) {
		services := []*types.AppServiceBuildConfig{
			{Name: "nginx"},
			{Name: "php", Main: true},
			{Name: "redis"},
		}

		got := prioritizeMainService(services)

		want := []string{"php", "nginx", "redis"}
		for i, service := range got {
			if service.Name != want[i] {
				t.Fatalf("service at index %d = %q, want %q", i, service.Name, want[i])
			}
		}
	})

	t.Run("keeps order when main service is already first", func(t *testing.T) {
		services := []*types.AppServiceBuildConfig{
			{Name: "php", Main: true},
			{Name: "nginx"},
		}

		got := prioritizeMainService(services)

		if got[0].Name != "php" || got[1].Name != "nginx" {
			t.Fatalf("service order = [%q %q], want [php nginx]", got[0].Name, got[1].Name)
		}
	})

	t.Run("keeps order when there is no main service", func(t *testing.T) {
		services := []*types.AppServiceBuildConfig{
			{Name: "php"},
			{Name: "nginx"},
		}

		got := prioritizeMainService(services)

		if got[0].Name != "php" || got[1].Name != "nginx" {
			t.Fatalf("service order = [%q %q], want [php nginx]", got[0].Name, got[1].Name)
		}
	})
}

func TestAppBuildImageTagIncludesUniqueBuildID(t *testing.T) {
	config := &types.Config{
		AppBuild: types.AppBuild{
			ID:     types.ToID(123),
			Number: 42,
			Config: &types.AppBuildConfig{
				RegistryHost:       "registry.example.com",
				RegistryRepository: "apps/demo",
			},
		},
	}

	got := appBuildImageTag(config, "php")
	want := "registry.example.com/apps/demo:php-42-123"
	if got != want {
		t.Fatalf("appBuildImageTag() = %q, want %q", got, want)
	}

	config.AppBuild.ID = types.ToID(124)
	if next := appBuildImageTag(config, "php"); next == got {
		t.Fatalf("appBuildImageTag() reused %q for a different app-build ID", next)
	}
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

func TestDockerfileArgNames(t *testing.T) {
	dockerfile := `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_TO=/var/www/html
ARG APP_ENV=prod
  arg EXTRA
RUN echo done`

	got := dockerfileArgNames(dockerfile)
	want := []string{"WODBY_BASE_IMAGE", "COPY_TO", "APP_ENV", "EXTRA"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dockerfileArgNames() = %v, want %v", got, want)
	}
}

func TestAddDockerfileBuildArgsUsesArgNameWithoutDefault(t *testing.T) {
	buildArgs := map[string]string{}
	appServiceBuildConfig := &types.AppServiceBuildConfig{
		Args: []*types.AppServiceBuildArg{
			{Name: "APP_ENV", Value: "stage"},
		},
	}
	dockerfile := `ARG COPY_TO=/var/www/html
ARG APP_ENV=prod`

	var redactValues []string
	err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, "/srv/app", log.NewEntry(log.New()), &redactValues)
	if err != nil {
		t.Fatalf("addDockerfileBuildArgs() error = %v", err)
	}

	if got := buildArgs["COPY_TO"]; got != "/srv/app" {
		t.Fatalf("COPY_TO build arg = %q, want %q", got, "/srv/app")
	}
	if got := buildArgs["APP_ENV"]; got != "stage" {
		t.Fatalf("APP_ENV build arg = %q, want %q", got, "stage")
	}
}

func TestAddDockerfileBuildArgsForSecretUsesEnvForwarding(t *testing.T) {
	t.Setenv("APP_SECRET", "very-secret-value")

	buildArgs := map[string]string{}
	appServiceBuildConfig := &types.AppServiceBuildConfig{
		Args: []*types.AppServiceBuildArg{
			{Name: "APP_SECRET", Secret: true},
		},
	}
	dockerfile := `ARG APP_SECRET`

	var redactValues []string
	err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, "/srv/app", log.NewEntry(log.New()), &redactValues)
	if err != nil {
		t.Fatalf("addDockerfileBuildArgs() error = %v", err)
	}

	if got := buildArgs["APP_SECRET"]; got != "" {
		t.Fatalf("APP_SECRET build arg = %q, want empty env-forwarding value", got)
	}
	if !reflect.DeepEqual(redactValues, []string{"very-secret-value"}) {
		t.Fatalf("redactValues = %v, want [very-secret-value]", redactValues)
	}
}

func TestAddDockerfileBuildArgsForSecretRequiresEnv(t *testing.T) {
	_ = os.Unsetenv("APP_SECRET")

	buildArgs := map[string]string{}
	appServiceBuildConfig := &types.AppServiceBuildConfig{
		Args: []*types.AppServiceBuildArg{
			{Name: "APP_SECRET", Secret: true},
		},
	}
	dockerfile := `ARG APP_SECRET`

	var redactValues []string
	err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, "/srv/app", log.NewEntry(log.New()), &redactValues)
	if err == nil {
		t.Fatal("addDockerfileBuildArgs() error = nil, want missing env error")
	}
}

func TestLayersDerivedFrom(t *testing.T) {
	base := []string{"sha256:a", "sha256:b", "sha256:c"}

	for _, tc := range []struct {
		name  string
		built []string
		want  bool
	}{
		{name: "derived with extra layers", built: []string{"sha256:a", "sha256:b", "sha256:c", "sha256:d"}, want: true},
		{name: "identical", built: base, want: true},
		{name: "unrelated base", built: []string{"sha256:x", "sha256:y"}, want: false},
		{name: "shares a prefix but diverges", built: []string{"sha256:a", "sha256:x", "sha256:c", "sha256:d"}, want: false},
		{name: "shorter than base", built: []string{"sha256:a", "sha256:b"}, want: false},
		{name: "base layers appear later, not as a prefix", built: []string{"sha256:z", "sha256:a", "sha256:b", "sha256:c"}, want: false},
		{name: "empty built", built: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := layersDerivedFrom(base, tc.built); got != tc.want {
				t.Fatalf("layersDerivedFrom() = %v, want %v", got, tc.want)
			}
		})
	}

	if layersDerivedFrom(nil, []string{"sha256:a"}) {
		t.Fatal("layersDerivedFrom() with no base layers = true, want false")
	}
}

func TestAuthoredDockerfileOnlyCoversRepositoryDockerfiles(t *testing.T) {
	for source, want := range map[string]bool{
		dockerfileSourceFlag:    true,
		dockerfileSourceContext: true,
		dockerfileSourceService: false,
		dockerfileSourceDefault: false,
	} {
		if got := authoredDockerfile(source); got != want {
			t.Fatalf("authoredDockerfile(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestDockerfileContentHash(t *testing.T) {
	const dockerfile = "ARG WODBY_BASE_IMAGE\nFROM ${WODBY_BASE_IMAGE}\n"

	got := dockerfileContentHash(dockerfile)
	if !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("dockerfileContentHash() = %q, want a sha256: prefix", got)
	}
	if got != dockerfileContentHash(dockerfile) {
		t.Fatal("dockerfileContentHash() is not stable for identical content")
	}
	if got == dockerfileContentHash(dockerfile+"RUN true\n") {
		t.Fatal("dockerfileContentHash() collided for different content")
	}
	if dockerfileContentHash("") != "" {
		t.Fatalf("dockerfileContentHash(\"\") = %q, want empty", dockerfileContentHash(""))
	}
}

func TestJoinCopyPath(t *testing.T) {
	for _, tc := range []struct {
		name   string
		root   string
		subdir string
		want   string
	}{
		{name: "default root, no subdir", root: ".", subdir: "", want: "."},
		{name: "default root with subdir", root: ".", subdir: "web", want: "web"},
		{name: "explicit root with subdir", root: "static", subdir: "web", want: "static/web"},
		{name: "absolute destination with subdir", root: "/var/www/html", subdir: "web", want: "/var/www/html/web"},
		{name: "explicit root, no subdir", root: "static", subdir: "", want: "static"},
		{name: "empty root, no subdir", root: "", subdir: "", want: "."},
		{name: "empty root with subdir", root: "", subdir: "web", want: "web"},
		{name: "nested subdir", root: ".", subdir: "apps/web", want: "apps/web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinCopyPath(tc.root, tc.subdir); got != tc.want {
				t.Fatalf("joinCopyPath(%q, %q) = %q, want %q", tc.root, tc.subdir, got, tc.want)
			}
		})
	}
}
