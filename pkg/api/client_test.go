package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestNewCIBuildUsesRESTEndpoint(t *testing.T) {
	var requestedPath string
	var apiKey string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		apiKey = r.Header.Get("X-API-KEY")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         101,
			"number":     7,
			"gitRefType": "BRANCH",
			"gitRef":     "main",
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Key: "secret", Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	build, err := client.NewCIBuild(context.Background(), types.NewBuildFromCIInput{
		AppServiceID: types.ToID(42),
		GitCommitSHA: "abc123",
		GitRef:       "main",
		GitRefType:   "BRANCH",
		BuildNum:     7,
		BuildID:      "build-7",
		Provider:     "github",
	})
	if err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-builds/from-ci" {
		t.Fatalf("path = %q, want /v1/app-builds/from-ci", requestedPath)
	}
	if apiKey != "secret" {
		t.Fatalf("api key = %q, want secret", apiKey)
	}
	if body["appServiceId"] != float64(42) {
		t.Fatalf("appServiceId = %#v, want 42", body["appServiceId"])
	}
	if _, ok := body["appServiceID"]; ok {
		t.Fatal("request body used legacy appServiceID key")
	}
	if body["buildId"] != "build-7" {
		t.Fatalf("buildId = %#v, want build-7", body["buildId"])
	}
	if build.ID != "101" {
		t.Fatalf("build ID = %q, want 101", build.ID)
	}
}

func TestGetAppBuildConfigUsesRESTEndpoint(t *testing.T) {
	var requestedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"registryHost":       "registry.example.com",
			"registryRepository": "apps/demo",
			"services": []map[string]interface{}{
				{
					"name":    "php",
					"title":   "PHP",
					"image":   "php:latest",
					"managed": true,
					"main":    true,
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{AccessToken: "token", Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := client.GetAppBuildConfig(context.Background(), types.ToID(101))
	if err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-builds/101/config" {
		t.Fatalf("path = %q, want /v1/app-builds/101/config", requestedPath)
	}
	if config.RegistryHost != "registry.example.com" {
		t.Fatalf("registry host = %q, want registry.example.com", config.RegistryHost)
	}
	if len(config.Services) != 1 || config.Services[0].Name != "php" || !config.Services[0].Main {
		t.Fatalf("services = %#v, want main php service", config.Services)
	}
}

func TestDeployUsesRESTEndpoint(t *testing.T) {
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 303})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Key: "secret", Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}

	deployment, err := client.Deploy(context.Background(), types.DeploymentFromCIInput{
		AppBuildID: types.ToID(101),
		Services: []*types.ServiceDeploymentInput{
			{Name: "php", Image: "registry.example.com/apps/demo:php"},
		},
		SkipPostDeployment: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-deployments/from-ci" {
		t.Fatalf("path = %q, want /v1/app-deployments/from-ci", requestedPath)
	}
	if body["appBuildId"] != float64(101) {
		t.Fatalf("appBuildId = %#v, want 101", body["appBuildId"])
	}
	if _, ok := body["appBuildID"]; ok {
		t.Fatal("request body used legacy appBuildID key")
	}
	if deployment.ID != "303" {
		t.Fatalf("deployment ID = %q, want 303", deployment.ID)
	}
}
