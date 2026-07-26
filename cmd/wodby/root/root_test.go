package root

import "testing"

func TestAPIEndpointDefaults(t *testing.T) {
	cmd := NewCommand()

	ciEndpoint := cmd.PersistentFlags().Lookup("api-endpoint")
	if ciEndpoint == nil {
		t.Fatal("api-endpoint flag was not registered")
	}
	if ciEndpoint.DefValue != "" {
		t.Fatalf("api-endpoint default = %q, want empty deprecated alias default", ciEndpoint.DefValue)
	}

	restBaseURL := cmd.PersistentFlags().Lookup("api-base-url")
	if restBaseURL == nil {
		t.Fatal("api-base-url flag was not registered")
	}
	if restBaseURL.DefValue != "https://apiv2.wodby.com/v1" {
		t.Fatalf("api-base-url default = %q, want apiv2 REST API base URL", restBaseURL.DefValue)
	}
}

func TestMigrateCommandVisibleAndOnlyExposesAppMigration(t *testing.T) {
	cmd := NewCommand()

	migrate, _, err := cmd.Find([]string{"migrate"})
	if err != nil {
		t.Fatal(err)
	}
	if migrate == nil || migrate.Name() != "migrate" {
		t.Fatalf("unexpected command = %#v", migrate)
	}
	if migrate.Hidden {
		t.Fatal("customer migrate command must be visible")
	}

	app, _, err := cmd.Find([]string{"migrate", "wodby1", "app"})
	if err != nil {
		t.Fatal(err)
	}
	if app == nil || app.Use != "app SOURCE_APP_UUID" {
		t.Fatalf("unexpected app command = %#v", app)
	}

	wodby1 := app.Parent()
	if wodby1 == nil || wodby1.Name() != "wodby1" {
		t.Fatalf("unexpected Wodby 1 command = %#v", wodby1)
	}
	for _, child := range wodby1.Commands() {
		if child.Name() == "server" {
			t.Fatal("server-wide migration must not be exposed to customers")
		}
	}
}
