package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
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

func TestFindMainServiceBuildConfig(t *testing.T) {
	t.Run("finds main service", func(t *testing.T) {
		service, err := findMainServiceBuildConfig([]*types.AppServiceBuildConfig{
			{Name: "php"},
			{Name: "nginx", Main: true},
		})
		if err != nil {
			t.Fatalf("findMainServiceBuildConfig() error = %v", err)
		}
		if service.Name != "nginx" {
			t.Fatalf("findMainServiceBuildConfig() name = %q, want %q", service.Name, "nginx")
		}
	})

	t.Run("missing main service", func(t *testing.T) {
		_, err := findMainServiceBuildConfig([]*types.AppServiceBuildConfig{
			{Name: "php"},
		})
		if err == nil {
			t.Fatal("findMainServiceBuildConfig() error = nil, want error")
		}
	})
}

func TestShouldFixPermissions(t *testing.T) {
	testCases := []struct {
		name             string
		isCI             bool
		explicit         bool
		managed          bool
		hasDataContainer bool
		want             bool
	}{
		{
			name:             "disabled outside CI",
			isCI:             false,
			explicit:         true,
			managed:          true,
			hasDataContainer: true,
			want:             false,
		},
		{
			name:             "enabled explicitly on bind mount",
			isCI:             true,
			explicit:         true,
			managed:          false,
			hasDataContainer: false,
			want:             true,
		},
		{
			name:             "enabled automatically for managed service in data container",
			isCI:             true,
			explicit:         false,
			managed:          true,
			hasDataContainer: true,
			want:             true,
		},
		{
			name:             "disabled automatically for managed service on host workspace",
			isCI:             true,
			explicit:         false,
			managed:          true,
			hasDataContainer: false,
			want:             false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFixPermissions(tc.isCI, tc.explicit, tc.managed, tc.hasDataContainer); got != tc.want {
				t.Fatalf("shouldFixPermissions() = %v, want %v", got, tc.want)
			}
		})
	}
}
