package releaseworkflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWodby2ReleaseChannels(t *testing.T) {
	workflow := readWorkflow(t)

	for _, want := range []string{
		"latest=false",
		"type=raw,value=2.0-dev,enable=${{ github.ref == 'refs/heads/2.0' }}",
		"type=raw,value=2.0,enable=${{ github.ref_type == 'tag' && startsWith(github.ref_name, '2.') }}",
		"type=ref,event=tag",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow does not preserve the Wodby 2 channel contract %q", want)
		}
	}

	if strings.Contains(workflow, "type=raw,value=latest") {
		t.Fatal("Wodby 2 release workflow must not publish the Wodby 1 latest image channel")
	}
	if strings.Contains(workflow, "--latest=false") {
		t.Fatal("stable Wodby 2 tags must remain eligible to be the latest GitHub Release")
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
