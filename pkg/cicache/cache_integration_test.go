package cicache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDataContainerCacheIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}

	context := t.TempDir()
	t.Setenv("WODBY_CI_CACHE_DIR", "")
	input := filepath.Join(context, DirectoryName, "npm", "restored")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("restored"), 0644); err != nil {
		t.Fatal(err)
	}

	container := fmt.Sprintf("wodby-ci-cache-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-fv", container).Run()
	})
	if output, err := exec.Command("docker", "pull", "alpine").CombinedOutput(); err != nil {
		t.Fatalf("docker pull alpine: %v\n%s", err, output)
	}
	if output, err := exec.Command(
		"docker", "create",
		"--name="+container,
		"--volume=/workspace",
		"--volume="+ContainerRoot,
		"alpine", "/bin/true",
	).CombinedOutput(); err != nil {
		t.Fatalf("docker create: %v\n%s", err, output)
	}
	if output, err := exec.Command("docker", "cp", context+string(os.PathSeparator)+".", container+":/workspace").CombinedOutput(); err != nil {
		t.Fatalf("docker cp context: %v\n%s", err, output)
	}

	if err := ImportDataContainer(container, "/workspace", context); err != nil {
		t.Fatal(err)
	}
	if err := PrepareDataContainerProfiles(container, []string{"npm"}); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"docker", "run", "--rm",
		"--volumes-from="+container,
		"alpine", "sh", "-eu", "-c",
		"test ! -e /workspace/.wodby-ci-cache; test -f /tmp/wodby-cache/npm/restored; echo updated > /tmp/wodby-cache/npm/updated",
	).CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, output)
	}
	if err := ExportDataContainerProfiles(container, context, []string{"npm"}); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(context, DirectoryName, "npm", "updated")); err != nil || string(content) != "updated\n" {
		t.Fatalf("exported cache = %q, %v", content, err)
	}
}
