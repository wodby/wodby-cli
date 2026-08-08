package request

import "testing"

func TestNewClientSetsHTTPTimeout(t *testing.T) {
	client := newHTTPClient()
	if client.Timeout != defaultHTTPTimeout {
		t.Fatalf("HTTP client timeout = %s, want %s", client.Timeout, defaultHTTPTimeout)
	}
}
