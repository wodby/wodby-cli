package run

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestResolveRunUser(t *testing.T) {
	t.Run("uses explicit user override", func(t *testing.T) {
		got, err := resolveRunUser("root", &types.Config{})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "root" {
			t.Fatalf("resolveRunUser() = %q, want %q", got, "root")
		}
	})

	t.Run("skips host user mapping for data container", func(t *testing.T) {
		got, err := resolveRunUser("", &types.Config{DataContainer: "data"})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}
		if got != "" {
			t.Fatalf("resolveRunUser() = %q, want empty user", got)
		}
	})

	t.Run("uses host owner for bind mounted context", func(t *testing.T) {
		dir := t.TempDir()

		got, err := resolveRunUser("", &types.Config{Context: dir})
		if err != nil {
			t.Fatalf("resolveRunUser() error = %v", err)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("os.Stat() error = %v", err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		want := fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)

		if got != want {
			t.Fatalf("resolveRunUser() = %q, want %q", got, want)
		}
	})

	t.Run("returns stat errors", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")

		_, err := resolveRunUser("", &types.Config{Context: missing})
		if err == nil {
			t.Fatal("resolveRunUser() error = nil, want error")
		}
	})
}

func TestWithDefaultSkipCodebaseChownEnv(t *testing.T) {
	t.Run("adds default env when absent", func(t *testing.T) {
		got := withDefaultSkipCodebaseChownEnv([]string{"FOO=bar"})
		want := []string{"FOO=bar", "WODBY_SKIP_CODEBASE_CHOWN=1"}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("withDefaultSkipCodebaseChownEnv() = %#v, want %#v", got, want)
		}
	})

	t.Run("preserves explicit env value", func(t *testing.T) {
		got := withDefaultSkipCodebaseChownEnv([]string{"WODBY_SKIP_CODEBASE_CHOWN=0"})
		want := []string{"WODBY_SKIP_CODEBASE_CHOWN=0"}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("withDefaultSkipCodebaseChownEnv() = %#v, want %#v", got, want)
		}
	})
}
