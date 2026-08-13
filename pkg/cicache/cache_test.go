package cicache

import (
	"path/filepath"
	"testing"
)

func TestHostRootDefaultsToContext(t *testing.T) {
	t.Setenv("WODBY_CI_CACHE_DIR", "")
	context := t.TempDir()

	got, err := HostRoot(context)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(context, DirectoryName)
	if got != want {
		t.Fatalf("HostRoot() = %q, want %q", got, want)
	}
}

func TestHostRootResolvesExplicitRelativePath(t *testing.T) {
	t.Setenv("WODBY_CI_CACHE_DIR", "relative-cache")

	got, err := HostRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("HostRoot() = %q, want absolute path", got)
	}
}
