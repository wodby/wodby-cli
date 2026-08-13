package cidata

import (
	"reflect"
	"testing"
)

func TestUtilityCommandUsesDataContainerVolumes(t *testing.T) {
	cmd := utilityCommand("data-container", "chown", "-R", "1001:1002", "/workspace")
	want := []string{
		"docker", "run", "--rm",
		"--volumes-from=data-container",
		"--user=root",
		"--entrypoint=",
		"alpine",
		"chown", "-R", "1001:1002", "/workspace",
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("utility command = %v, want %v", cmd.Args, want)
	}
}
