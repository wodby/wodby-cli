package releaseworkflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWodby1ReleaseChannels(t *testing.T) {
	workflow := readWorkflow(t)

	for _, want := range []string{
		"tags: wodby/wodby-cli:dev",
		"tags: wodby/wodby-cli:${{ env.VERSION }},wodby/wodby-cli:latest",
		"--latest=false",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow does not preserve the Wodby 1 channel contract %q", want)
		}
	}

	if strings.Contains(workflow, "wodby/wodby-cli:2.0") {
		t.Fatal("Wodby 1 release workflow must not publish the Wodby 2 image channel")
	}
}

func readWorkflow(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", ".github", "workflows", "workflow.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
