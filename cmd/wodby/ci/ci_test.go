package ci

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Exercise command routing, service selection, and tagging through both names
// without contacting Docker or a registry.
func TestPushAndReleaseAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Docker executable uses a POSIX shell")
	}
	for _, name := range []string{"push", "release"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dockerLog := filepath.Join(dir, "docker.log")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_TEST_LOG\"\n"
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DOCKER_TEST_LOG", dockerLog)
			configPath := filepath.Join(dir, "ci.json")
			if err := os.WriteFile(configPath, []byte(`{"BuildConfig":{"services":{"php":{"name":"php","slug":"registry.example.com/app"},"nginx":{"name":"nginx","slug":"registry.example.com/nginx"}},"registry":{"host":"registry.example.com","username":"test","password":"test"}},"metadata":{"number":"42","branch":"main"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			viper.Set("ci_config_path", configPath)
			t.Cleanup(viper.Reset)
			root := &cobra.Command{Use: "wodby"}
			root.AddCommand(Cmd)
			root.SetArgs([]string{"ci", name, "php", "--branch-tag", "--latest-branch", "main"})
			command, err := root.ExecuteC()
			if err != nil {
				t.Fatal(err)
			}
			if command.Name() != "push" {
				t.Fatalf("resolved command = %q, want push", command.Name())
			}
			calls, err := os.ReadFile(dockerLog)
			if err != nil {
				t.Fatal(err)
			}
			want := "login -u test --password-stdin registry.example.com\npush registry.example.com/app:42\ntag registry.example.com/app:42 registry.example.com/app:latest\npush registry.example.com/app:latest\ntag registry.example.com/app:42 registry.example.com/app:main\npush registry.example.com/app:main\n"
			if string(calls) != want {
				t.Fatalf("Docker calls:\n%s\nwant:\n%s", calls, want)
			}
		})
	}
}

func TestCIHelpListsPush(t *testing.T) {
	t.Cleanup(func() {
		if flag := Cmd.Flags().Lookup("help"); flag != nil {
			if err := flag.Value.Set("false"); err != nil {
				t.Error(err)
			}
		}
	})
	var output strings.Builder
	root := &cobra.Command{Use: "wodby"}
	root.AddCommand(Cmd)
	root.SetOut(&output)
	root.SetArgs([]string{"ci", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "push") || strings.Contains(output.String(), "release") {
		t.Fatalf("CI help must list push as the primary command:\n%s", output.String())
	}
}
