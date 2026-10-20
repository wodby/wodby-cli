package migrate

import (
	"testing"
)

func TestWorkspaceFlagsMatchCommandScope(t *testing.T) {
	instance := newWodby1InstanceCommand()
	if instance.Flags().Lookup("workspace") == nil || instance.Flags().Lookup("workspace-instance") != nil {
		t.Fatal("instance migration must expose only --workspace")
	}
	for _, cmd := range []string{"app", "server"} {
		command := newWodby1AppCommand()
		if cmd == "server" {
			command = newWodby1ServerCommand()
		}
		if command.Flags().Lookup("workspace") != nil || command.Flags().Lookup("workspace-instance") == nil {
			t.Fatalf("%s migration must expose only --workspace-instance", cmd)
		}
	}
}
