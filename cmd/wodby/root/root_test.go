package root

import "testing"

func TestAPIEndpointDefaults(t *testing.T) {
	cmd := NewCommand()

	ciEndpoint := cmd.PersistentFlags().Lookup("api-endpoint")
	if ciEndpoint == nil {
		t.Fatal("api-endpoint flag was not registered")
	}
	if ciEndpoint.DefValue != "https://apiv2.wodby.com/query" {
		t.Fatalf("api-endpoint default = %q, want legacy CI API endpoint", ciEndpoint.DefValue)
	}

	restBaseURL := cmd.PersistentFlags().Lookup("api-base-url")
	if restBaseURL == nil {
		t.Fatal("api-base-url flag was not registered")
	}
	if restBaseURL.DefValue != "https://api.wodby.com/v1" {
		t.Fatalf("api-base-url default = %q, want public REST API base URL", restBaseURL.DefValue)
	}
}
