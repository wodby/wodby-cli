package ops

import "testing"

func TestCommandsExposeTopLevelOperationalSurface(t *testing.T) {
	cmds := Commands()
	names := make(map[string]bool)
	for _, cmd := range cmds {
		names[cmd.Name()] = true
	}

	for _, name := range []string{
		"org",
		"project",
		"env",
		"app",
		"instance",
		"build",
		"deployment",
		"backup",
		"import",
		"task",
		"route",
	} {
		if !names[name] {
			t.Fatalf("missing command %q", name)
		}
	}

	if names["service"] {
		t.Fatal("top-level service command should not be registered")
	}
}

func TestAppCommandExposesCanonicalNestedResources(t *testing.T) {
	app := newAppCommand()
	names := make(map[string]bool)
	for _, cmd := range app.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "status", "instance", "service", "route"} {
		if !names[name] {
			t.Fatalf("missing app subcommand %q", name)
		}
	}
}
