package api

import (
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestNewHTTPClientSetsTimeout(t *testing.T) {
	httpClient := newHTTPClient(types.APIConfig{})

	if httpClient.Timeout != defaultHTTPTimeout {
		t.Fatalf("HTTP client timeout = %s, want %s", httpClient.Timeout, defaultHTTPTimeout)
	}
}
