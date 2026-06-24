package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

func TestNewCIBuildUsesGraphQLEndpointAndLegacyInputKeys(t *testing.T) {
	var requestedPath string
	var apiKey string
	var request graphQLRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		apiKey = r.Header.Get("X-API-KEY")
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"newBuildFromCI": map[string]interface{}{
					"id":         "101",
					"number":     7,
					"gitRefType": "BRANCH",
					"gitRef":     "main",
					"config":     appBuildConfigResponse(),
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Key: "secret", Endpoint: server.URL + "/query"})
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

	if requestedPath != "/query" {
		t.Fatalf("path = %q, want /query", requestedPath)
	}
	if apiKey != "secret" {
		t.Fatalf("api key = %q, want secret", apiKey)
	}
	if !strings.Contains(request.Query, "newBuildFromCI") {
		t.Fatalf("query = %q, want newBuildFromCI mutation", request.Query)
	}

	input := request.Variables["input"].(map[string]interface{})
	if input["appServiceID"] != "42" {
		t.Fatalf("appServiceID = %#v, want 42", input["appServiceID"])
	}
	if _, ok := input["appServiceId"]; ok {
		t.Fatal("request input used REST appServiceId key")
	}
	if input["buildID"] != "build-7" {
		t.Fatalf("buildID = %#v, want build-7", input["buildID"])
	}
	if _, ok := input["buildId"]; ok {
		t.Fatal("request input used REST buildId key")
	}
	if build.ID != "101" {
		t.Fatalf("build ID = %q, want 101", build.ID)
	}
}

func TestGetAppBuildConfigUsesGraphQLAndAccessToken(t *testing.T) {
	var requestedPath string
	var accessToken string
	var request graphQLRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		accessToken = r.Header.Get("X-ACCESS-TOKEN")
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"appBuild": map[string]interface{}{
					"id":         "101",
					"number":     7,
					"gitRefType": "BRANCH",
					"gitRef":     "main",
					"config":     appBuildConfigResponse(),
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{AccessToken: "token", Endpoint: server.URL + "/query"})
	if err != nil {
		t.Fatal(err)
	}

	config, err := client.GetAppBuildConfig(context.Background(), types.ToID(101))
	if err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/query" {
		t.Fatalf("path = %q, want /query", requestedPath)
	}
	if accessToken != "token" {
		t.Fatalf("access token = %q, want token", accessToken)
	}
	if !strings.Contains(request.Query, "appBuild") {
		t.Fatalf("query = %q, want appBuild query", request.Query)
	}
	if config.RegistryHost != "registry.example.com" {
		t.Fatalf("registry host = %q, want registry.example.com", config.RegistryHost)
	}
	if len(config.Services) != 1 || config.Services[0].Name != "php" || !config.Services[0].Main {
		t.Fatalf("services = %#v, want main php service", config.Services)
	}
}

func TestDeployUsesGraphQLAndLegacyInputKeys(t *testing.T) {
	var request graphQLRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"deployFromCI": map[string]interface{}{
					"id": "303",
				},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(types.APIConfig{Key: "secret", Endpoint: server.URL + "/query"})
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

	if !strings.Contains(request.Query, "deployFromCI") {
		t.Fatalf("query = %q, want deployFromCI mutation", request.Query)
	}
	input := request.Variables["input"].(map[string]interface{})
	if input["appBuildID"] != "101" {
		t.Fatalf("appBuildID = %#v, want 101", input["appBuildID"])
	}
	if _, ok := input["appBuildId"]; ok {
		t.Fatal("request input used REST appBuildId key")
	}
	if deployment.ID != "303" {
		t.Fatalf("deployment ID = %q, want 303", deployment.ID)
	}
}

func appBuildConfigResponse() map[string]interface{} {
	return map[string]interface{}{
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
	}
}
