package docker

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestResolveImageUserIdentityIntegration(t *testing.T) {
	if os.Getenv("WODBY_DOCKER_INTEGRATION") == "" {
		t.Skip("set WODBY_DOCKER_INTEGRATION=1 to run Docker integration tests")
	}
	image := fmt.Sprintf("wodby-ci-user-test:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("docker", "image", "rm", image).Run()
	})

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	files := map[string]string{
		"etc/passwd": "app:x:1234:5678:app:/workspace:/sbin/nologin\n",
		"etc/group":  "app:x:5678:\n",
	}
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("docker", "import", "--change", "USER app", "-", image)
	command.Stdin = &archive
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("docker import minimal image: %v\n%s", err, output)
	}

	uid, gid, err := NewClient().ResolveImageUserIdentity(image, "app")
	if err != nil {
		t.Fatal(err)
	}
	if uid != 1234 || gid != 5678 {
		t.Fatalf("app identity = %d:%d, want 1234:5678", uid, gid)
	}
}
