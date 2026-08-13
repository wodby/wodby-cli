package cidata

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestFixOwnershipIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}
	container := fmt.Sprintf("wodby-ci-ownership-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-fv", container).Run()
	})
	if output, err := exec.Command("docker", "pull", "alpine").CombinedOutput(); err != nil {
		t.Fatalf("docker pull alpine: %v\n%s", err, output)
	}
	if output, err := exec.Command(
		"docker", "create", "--name="+container, "--volume=/workspace", "alpine", "/bin/true",
	).CombinedOutput(); err != nil {
		t.Fatalf("docker create: %v\n%s", err, output)
	}

	if err := FixOwnership(container, "/workspace", "1234:5678"); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(
		"docker", "run", "--rm", "--volumes-from="+container,
		"--entrypoint=", "alpine", "stat", "-c", "%u:%g", "/workspace",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("docker stat: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "1234:5678" {
		t.Fatalf("workspace ownership = %s, want 1234:5678", got)
	}
}
