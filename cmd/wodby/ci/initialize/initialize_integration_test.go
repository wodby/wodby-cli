package initialize

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wodby/wodby-cli/pkg/ciuser"
	"github.com/wodby/wodby-cli/pkg/docker"
)

func TestManagedInitPreservesDrupalEntrypointIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}

	const image = "wodby/drupal-php:8.3-4.82.4"
	if output, err := exec.Command("docker", "pull", image).CombinedOutput(); err != nil {
		t.Fatalf("docker pull Drupal PHP image: %v\n%s", err, output)
	}

	t.Run("native bind mount restores checkout ownership", func(t *testing.T) {
		context := t.TempDir()
		siteDir := filepath.Join(context, "web", "sites", "default")
		if err := os.MkdirAll(siteDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(siteDir, "default.settings.php"), []byte("<?php\n"), 0644); err != nil {
			t.Fatal(err)
		}

		originalOwnership, err := ciuser.WorkspaceOwner(context)
		if err != nil {
			t.Fatal(err)
		}
		defaultUser, err := docker.NewClient().GetImageDefaultUser(image)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixBindOwnership(docker.NewClient(), image, "/var/www/html", context, docker.ChownSpec(defaultUser)); err != nil {
			t.Fatal(err)
		}
		restored := false
		defer func() {
			if !restored {
				_ = fixBindOwnership(docker.NewClient(), image, "/var/www/html", context, originalOwnership)
			}
		}()

		runConfig := managedInitRunConfig(
			image,
			"/var/www/html",
			context,
			"",
			map[string]interface{}{
				"DOCROOT_SUBDIR": "web",
				"DRUPAL_SITE":    "default",
				"DRUPAL_VERSION": "10",
			},
		)
		if err := docker.NewClient().Run([]string{"make", "init-drupal"}, runConfig); err != nil {
			t.Fatal(err)
		}
		if err := fixBindOwnership(docker.NewClient(), image, "/var/www/html", context, originalOwnership); err != nil {
			t.Fatal(err)
		}
		restored = true

		if got, err := ciuser.WorkspaceOwner(context); err != nil || got != originalOwnership {
			t.Fatalf("restored checkout ownership = %q, %v; want %q", got, err, originalOwnership)
		}
		settingsPath := filepath.Join(siteDir, "settings.php")
		if got, err := ciuser.WorkspaceOwner(settingsPath); err != nil || got != originalOwnership {
			t.Fatalf("restored settings ownership = %q, %v; want %q", got, err, originalOwnership)
		}
		if info, err := os.Lstat(filepath.Join(siteDir, "files")); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("Drupal files path was not initialized as a symlink: %v", err)
		}
		if err := os.WriteFile(filepath.Join(context, ".dockerignore"), []byte(".git\n"), 0644); err != nil {
			t.Fatalf("restored checkout is not writable by the host: %v", err)
		}
	})

	t.Run("Docker-in-Docker data volume", func(t *testing.T) {
		container := fmt.Sprintf("wodby-ci-init-test-%d", time.Now().UnixNano())
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-fv", container).Run()
		})
		if output, err := exec.Command(
			"docker", "create",
			"--name="+container,
			"--volume=/var/www/html",
			"alpine", "/bin/true",
		).CombinedOutput(); err != nil {
			t.Fatalf("docker create data container: %v\n%s", err, output)
		}

		runConfig := managedInitRunConfig(
			image,
			"/var/www/html",
			"",
			container,
			map[string]interface{}{
				"DOCROOT_SUBDIR": "web",
				"DRUPAL_SITE":    "default",
			},
		)
		if err := docker.NewClient().Run([]string{"make", "-n", "init-drupal"}, runConfig); err != nil {
			t.Fatal(err)
		}
	})
}
