package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestClientRejectsCrossOriginRedirectWithoutLeakingCredentials(t *testing.T) {
	destinationCalled := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalled = true
		if r.Header.Get("X-API-KEY") != "" || r.Header.Get("X-ACCESS-TOKEN") != "" {
			t.Fatal("API credentials leaked to redirect destination")
		}
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer destination.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/v1/user", http.StatusFound)
	}))
	defer redirect.Close()

	client, err := NewClient(types.APIConfig{Endpoint: redirect.URL + "/v1", Key: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	err = client.Get(context.Background(), "/user", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("err = %v", err)
	}
	if destinationCalled {
		t.Fatal("cross-origin redirect destination must not be requested")
	}
}

func TestClientSendsAPIKeyAndPreservesBasePath(t *testing.T) {
	var requestedPath string
	var requestedQuery string
	var apiKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		apiKey = r.Header.Get("X-API-KEY")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{
		Key:      "secret",
		Endpoint: server.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	query := url.Values{"appInstanceId": []string{"123"}}
	if err := client.Get(context.Background(), "/app-services", query, &out); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-services" {
		t.Fatalf("path = %q, want %q", requestedPath, "/v1/app-services")
	}
	if requestedQuery != "appInstanceId=123" {
		t.Fatalf("query = %q, want %q", requestedQuery, "appInstanceId=123")
	}
	if apiKey != "secret" {
		t.Fatalf("api key = %q, want %q", apiKey, "secret")
	}
}

func TestClientSendsAccessTokenWhenAPIKeyMissing(t *testing.T) {
	var accessToken string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessToken = r.Header.Get("X-ACCESS-TOKEN")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{
		AccessToken: "token",
		Endpoint:    server.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	if err := client.Get(context.Background(), "/orgs", nil, &out); err != nil {
		t.Fatal(err)
	}

	if accessToken != "token" {
		t.Fatalf("access token = %q, want %q", accessToken, "token")
	}
}

func TestClientDecodesAPIErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "bad request"})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	err = client.Get(context.Background(), "/orgs", nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "api request failed: bad request (400 Bad Request)" {
		t.Fatalf("error = %q", got)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Message != "bad request" {
		t.Fatalf("api error = %#v", apiErr)
	}
}

func TestClientDecodesProblemJSONErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"title":   "Invalid request",
			"detail":  "invalid request body",
			"message": "invalid request body",
			"errors": []map[string]interface{}{
				{"field": "name", "detail": "is required"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]interface{}
	err = client.Get(context.Background(), "/orgs", nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "api request failed: invalid request body: name: is required (400 Bad Request)" {
		t.Fatalf("error = %q", got)
	}
}
