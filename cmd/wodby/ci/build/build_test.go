package build

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestDataContainerWorkingDirContents(t *testing.T) {
	tests := []struct {
		workingDir string
		want       string
	}{
		{workingDir: "", want: "/."},
		{workingDir: "/", want: "/."},
		{workingDir: "/var/www/html", want: "/var/www/html/."},
		{workingDir: "/var/www/html/", want: "/var/www/html/."},
	}

	for _, tt := range tests {
		t.Run(tt.workingDir, func(t *testing.T) {
			if got := dataContainerWorkingDirContents(tt.workingDir); got != tt.want {
				t.Fatalf("dataContainerWorkingDirContents(%q) = %q, want %q", tt.workingDir, got, tt.want)
			}
		})
	}
}

func TestRenderDockerfileTemplateSupportsLegacyAndOwnershipVariables(t *testing.T) {
	tests := []struct {
		name        string
		dockerfile  string
		defaultUser string
		want        string
	}{
		{
			name:        "legacy default user",
			dockerfile:  "COPY --chown={{.DefaultUser}}:{{.DefaultUser}} source target",
			defaultUser: "wodby",
			want:        "COPY --chown=wodby:wodby source target",
		},
		{
			name:        "explicit user ownership",
			dockerfile:  "COPY --chown={{.DefaultUserOwnership}} source target",
			defaultUser: "wodby:www-data",
			want:        "COPY --chown=wodby:www-data source target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderDockerfileTemplate(tt.dockerfile, tt.defaultUser)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("renderDockerfileTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareDataContainerContextRejectsPathNames(t *testing.T) {
	for _, name := range []string{"", "../data", "nested/data"} {
		if _, err := prepareDataContainerContext(name); err == nil {
			t.Fatalf("prepareDataContainerContext(%q) error = nil, want error", name)
		}
	}
}

func TestEnsureDefaultDockerignore(t *testing.T) {
	t.Run("creates and removes a temporary default", func(t *testing.T) {
		context := t.TempDir()
		dockerignorePath := filepath.Join(context, ".dockerignore")

		cleanup, err := ensureDefaultDockerignore(context)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(dockerignorePath)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != Dockerignore {
			t.Fatalf(".dockerignore = %q, want %q", contents, Dockerignore)
		}

		cleanup()
		if _, err := os.Stat(dockerignorePath); !os.IsNotExist(err) {
			t.Fatalf("temporary .dockerignore still exists: %v", err)
		}
	})

	t.Run("preserves an existing file", func(t *testing.T) {
		context := t.TempDir()
		dockerignorePath := filepath.Join(context, ".dockerignore")
		if err := os.WriteFile(dockerignorePath, []byte("vendor\n"), 0600); err != nil {
			t.Fatal(err)
		}

		cleanup, err := ensureDefaultDockerignore(context)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(dockerignorePath)
		if err != nil {
			t.Fatal(err)
		}
		if !dockerignoreContains(string(contents), ".wodby-ci-cache") {
			t.Fatalf("temporary .dockerignore = %q, want cache exclusion", contents)
		}
		cleanup()

		contents, err = os.ReadFile(dockerignorePath)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "vendor\n" {
			t.Fatalf("existing .dockerignore changed to %q", contents)
		}
	})
}

func TestOrderedServicesPutsDefaultFirst(t *testing.T) {
	services := map[string]types.Service{
		"redis": {Name: "redis", Image: "redis"},
		"nginx": {Name: "nginx", Image: "nginx"},
		"php":   {Name: "php", Image: "php"},
	}

	got := orderedServices(services, "php")
	var names []string
	for _, service := range got {
		names = append(names, service.Name)
	}
	if want := []string{"php", "nginx", "redis"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("service order = %v, want %v", names, want)
	}
}

func TestOrderedBuildImagesPutsDefaultFirst(t *testing.T) {
	builds := map[string]*imageBuild{
		"redis": {},
		"nginx": {},
		"php":   {},
	}

	got := orderedBuildImages(builds, "php")
	want := []string{"php", "nginx", "redis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("image order = %v, want %v", got, want)
	}
}

func TestParseBuildArgPreservesEqualsInValue(t *testing.T) {
	name, value, err := parseBuildArg("TOKEN=first=second")
	if err != nil {
		t.Fatal(err)
	}
	if name != "TOKEN" || value != "first=second" {
		t.Fatalf("parseBuildArg() = %q, %q, want TOKEN, first=second", name, value)
	}
}
