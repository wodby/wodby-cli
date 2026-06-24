package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
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

func TestPermissionFixDecision(t *testing.T) {
	testCases := []struct {
		name       string
		explicit   bool
		want       bool
		wantReason string
	}{
		{
			name:       "enabled explicitly",
			explicit:   true,
			want:       true,
			wantReason: "requested explicitly with --fix-permissions",
		},
		{
			name:       "disabled by default",
			explicit:   false,
			want:       false,
			wantReason: "--fix-permissions was not set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := permissionFixDecision(tc.explicit)
			if got != tc.want {
				t.Fatalf("permissionFixDecision() = %v, want %v", got, tc.want)
			}
			if reason != tc.wantReason {
				t.Fatalf("permissionFixDecision() reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestProviderFlagHasShorthandAndNoDefault(t *testing.T) {
	flag := Cmd.Flags().Lookup("provider")
	if flag == nil {
		t.Fatal("provider flag was not registered")
	}
	if flag.Shorthand != "p" {
		t.Fatalf("provider shorthand = %q, want %q", flag.Shorthand, "p")
	}
	if flag.DefValue != "" {
		t.Fatalf("provider default = %q, want empty string", flag.DefValue)
	}
}

func TestNewCIAPIConfigUsesLegacyAPIEndpoint(t *testing.T) {
	viper.Set("api_key", "secret")
	viper.Set("access_token", "")
	viper.Set("api_endpoint", "https://ci.example.com/query")
	viper.Set("api_base_url", "https://api.example.com/v1")
	t.Cleanup(func() {
		viper.Set("api_key", "")
		viper.Set("access_token", "")
		viper.Set("api_endpoint", "")
		viper.Set("api_base_url", "")
	})

	config := newCIAPIConfig()
	if config.Endpoint != "https://ci.example.com/query" {
		t.Fatalf("endpoint = %q, want legacy CI API endpoint", config.Endpoint)
	}
	if config.Key != "secret" {
		t.Fatalf("key = %q, want secret", config.Key)
	}
}

func TestApplyCIBuildInputFlagsOverridesDetectedValues(t *testing.T) {
	input := types.NewBuildFromCIInput{
		Provider: "gitlab",
		BuildID:  "detected-build",
		BuildNum: 7,
	}

	err := applyCIBuildInputFlags(&input, options{
		provider:    "unknown",
		buildID:     "manual-build",
		buildNumber: 42,
	})
	if err != nil {
		t.Fatalf("applyCIBuildInputFlags() error = %v", err)
	}
	if input.Provider != "unknown" {
		t.Fatalf("provider = %q, want unknown", input.Provider)
	}
	if input.BuildID != "manual-build" {
		t.Fatalf("build ID = %q, want manual-build", input.BuildID)
	}
	if input.BuildNum != 42 {
		t.Fatalf("build num = %d, want 42", input.BuildNum)
	}
}

func TestApplyCIBuildInputFlagsKeepsCollectedValues(t *testing.T) {
	input := types.NewBuildFromCIInput{
		Provider: "unknown",
		BuildID:  "detected-build",
		BuildNum: 7,
	}

	err := applyCIBuildInputFlags(&input, options{})
	if err != nil {
		t.Fatalf("applyCIBuildInputFlags() error = %v", err)
	}
	if input.Provider != "unknown" {
		t.Fatalf("provider = %q, want unknown", input.Provider)
	}
	if input.BuildID != "detected-build" {
		t.Fatalf("build ID = %q, want detected-build", input.BuildID)
	}
	if input.BuildNum != 7 {
		t.Fatalf("build num = %d, want 7", input.BuildNum)
	}
}
