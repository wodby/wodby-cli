package build

import "testing"

func TestDataContainerContextPath(t *testing.T) {
	got := dataContainerContextPath("test-container")
	want := "/tmp/wodby-build-test-container"

	if got != want {
		t.Fatalf("dataContainerContextPath() = %q, want %q", got, want)
	}
}
