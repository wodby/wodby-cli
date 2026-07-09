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

func TestMigrateCommandHidden(t *testing.T) {
	cmd := NewCommand()

	migrate, _, err := cmd.Find([]string{"migrate", "wodby1", "app"})
	if err != nil {
		t.Fatal(err)
	}
	if migrate == nil || migrate.Use != "app SOURCE_APP_UUID" {
		t.Fatalf("unexpected command = %#v", migrate)
	}
	parent := migrate.Parent().Parent()
	if parent == nil || parent.Name() != "migrate" {
		t.Fatalf("unexpected parent command = %#v", parent)
	}
	if !parent.Hidden {
		t.Fatal("migrate command should be hidden")
	}
}
