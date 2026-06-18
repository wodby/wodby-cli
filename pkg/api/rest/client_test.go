package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

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
}
