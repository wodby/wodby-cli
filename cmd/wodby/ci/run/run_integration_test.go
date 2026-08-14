package run

import (
	"fmt"
	"os"
	"testing"

	"github.com/wodby/wodby-cli/pkg/docker"
)

func TestMatchingImageUIDPreservesEntrypointIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}
	// Public integration images do not need the developer machine's
	// credential helper, which may be unavailable in headless test runs.
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	const image = "wodby/drupal-php:8.3-4.82.4"
	dockerClient := docker.NewClient()
	imageConfig, err := dockerClient.GetImageConfig(image)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, err := dockerClient.ResolveImageUserIdentity(image, imageConfig.User)
	if err != nil {
		t.Fatal(err)
	}
	users := usersForImage(fmt.Sprintf("%d:%d", uid, gid), uid)
	if users.docker != "" {
		t.Fatalf("matching image UID produced Docker user override %q", users.docker)
	}
	if users.cache != fmt.Sprintf("%d:%d", uid, gid) {
		t.Fatalf("matching image UID lost cache identity: %q", users.cache)
	}

	runConfig := docker.RunConfig{
		Image: image,
		Env: []string{
			"DOCROOT_SUBDIR=web",
			"DRUPAL_SITE=default",
		},
		User:            users.docker,
		ClearEntrypoint: shouldClearImageEntrypoint("", users.docker),
	}
	if runConfig.ClearEntrypoint {
		t.Fatal("matching image UID unexpectedly clears the image entrypoint")
	}
	if err := dockerClient.Run([]string{"make", "-n", "init-drupal"}, runConfig); err != nil {
		t.Fatal(err)
	}
}

func TestDifferentWorkspaceUIDClearsEntrypointIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	const image = "wodby/drupal-php:8.3-4.82.4"
	dockerClient := docker.NewClient()
	imageConfig, err := dockerClient.GetImageConfig(image)
	if err != nil {
		t.Fatal(err)
	}
	imageUID, imageGID, err := dockerClient.ResolveImageUserIdentity(image, imageConfig.User)
	if err != nil {
		t.Fatal(err)
	}

	workspaceUID := imageUID + 1
	users := usersForImage(fmt.Sprintf("%d:%d", workspaceUID, imageGID), imageUID)
	if users.docker == "" {
		t.Fatal("different workspace UID unexpectedly preserved the image user")
	}

	runConfig := docker.RunConfig{
		Image:           image,
		User:            users.docker,
		ClearEntrypoint: shouldClearImageEntrypoint("", users.docker),
	}
	if !runConfig.ClearEntrypoint {
		t.Fatal("numeric workspace user must clear an incompatible image entrypoint")
	}
	if err := dockerClient.Run([]string{"sh", "-eu", "-c", fmt.Sprintf("test \"$(id -u)\" = %d", workspaceUID)}, runConfig); err != nil {
		t.Fatal(err)
	}
}
