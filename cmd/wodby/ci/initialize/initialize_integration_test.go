package initialize

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

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

	t.Run("native bind mount", func(t *testing.T) {
		runConfig := managedInitRunConfig(
			image,
			"/var/www/html",
			t.TempDir(),
			"",
			map[string]interface{}{
				"DOCROOT_SUBDIR": "web",
				"DRUPAL_SITE":    "default",
			},
		)
		if err := docker.NewClient().Run([]string{"make", "-n", "init-drupal"}, runConfig); err != nil {
			t.Fatal(err)
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
