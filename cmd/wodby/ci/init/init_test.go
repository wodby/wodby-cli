package init

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPostDeployment(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()

		content, err := readPostDeployment(dir)
		if err != nil {
			t.Fatalf("readPostDeployment() error = %v", err)
		}
		if content != "" {
			t.Fatalf("readPostDeployment() content = %q, want empty string", content)
		}
	})

	t.Run("existing file", func(t *testing.T) {
		dir := t.TempDir()
		postDeploymentDir := filepath.Join(dir, ".wodby")
		if err := os.Mkdir(postDeploymentDir, 0o755); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}

		want := "steps:\n  - echo hello\n"
		postDeploymentFile := filepath.Join(postDeploymentDir, "post-deployment.yml")
		if err := os.WriteFile(postDeploymentFile, []byte(want), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		content, err := readPostDeployment(dir)
		if err != nil {
			t.Fatalf("readPostDeployment() error = %v", err)
		}
		if content != want {
			t.Fatalf("readPostDeployment() content = %q, want %q", content, want)
		}
	})
}
