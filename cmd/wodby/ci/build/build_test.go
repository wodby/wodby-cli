package build

import (
	"path/filepath"
	"testing"
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
