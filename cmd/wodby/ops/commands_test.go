package ops

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
		"cluster",
		"integration",
		"provider",
		"stack",
		"service",
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

func TestNewCatalogCommandsExposeBasicReadOperations(t *testing.T) {
	for _, test := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "provider", cmd: newProviderCommand()},
		{name: "stack", cmd: newStackCommand()},
		{name: "service", cmd: newServiceCommand()},
	} {
		t.Run(test.name, func(t *testing.T) {
			names := make(map[string]bool)
			for _, cmd := range test.cmd.Commands() {
				names[cmd.Name()] = true
			}
			for _, name := range []string{"list", "get"} {
				if !names[name] {
					t.Fatalf("missing %s subcommand %q", test.name, name)
				}
			}
		})
	}
}

func TestClusterCreateUsesPublicAPIShape(t *testing.T) {
	var requestedMethod string
	var requestedPath string
	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedMethod = r.Method
		requestedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":            101,
			"name":          "prod",
			"title":         "Production",
			"integrationId": 7,
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newClusterCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--integration", "7",
		"--name", "prod",
		"--title", "Production",
		"--org", "10",
		"--project", "12",
		"--serverless",
		"--disable-monitoring",
		"--region", "us",
		"--min-node-count", "1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/clusters" {
		t.Fatalf("path = %q, want /v1/clusters", requestedPath)
	}
	if body["integrationId"] != float64(7) {
		t.Fatalf("integrationId = %#v, want 7", body["integrationId"])
	}
	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["projectId"] != float64(12) {
		t.Fatalf("projectId = %#v, want 12", body["projectId"])
	}
	if body["serverless"] != true {
		t.Fatalf("serverless = %#v, want true", body["serverless"])
	}
	if body["minNodeCount"] != float64(1) {
		t.Fatalf("minNodeCount = %#v, want 1", body["minNodeCount"])
	}
}

func TestProviderListSendsSupportedFilters(t *testing.T) {
	var requestedPath string
	var requestedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{"id": 1, "name": "aws", "title": "AWS"},
			},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newProviderCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"list",
		"--org", "10",
		"--project", "20,21",
		"--search", "aws",
		"--page", "2",
		"--page-size", "50",
		"--exclude-public",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/providers" {
		t.Fatalf("path = %q, want /v1/providers", requestedPath)
	}
	want := "excludePublic=true&orgId=10&page=2&pageSize=50&projectIds=20%2C21&search=aws"
	if requestedQuery != want {
		t.Fatalf("query = %q, want %q", requestedQuery, want)
	}
}

func TestInstanceListExcludesClusterAppsByDefault(t *testing.T) {
	query := executeInstanceListQuery(t, "list", "--org", "123")

	if got := query.Get("clusterApp"); got != "false" {
		t.Fatalf("clusterApp = %q, want false", got)
	}
}

func TestInstanceListCanFilterClusterApps(t *testing.T) {
	query := executeInstanceListQuery(t, "list", "--org", "123", "--cluster-app")

	if got := query.Get("clusterApp"); got != "true" {
		t.Fatalf("clusterApp = %q, want true", got)
	}
}

func executeInstanceListQuery(t *testing.T, args ...string) url.Values {
	t.Helper()

	var requestedPath string
	var requestedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 1, "name": "demo"},
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newAppInstanceCommand("instance", "Manage app instances")
	cmd.SetOut(io.Discard)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/v1/app-instances" {
		t.Fatalf("path = %q, want /v1/app-instances", requestedPath)
	}
	return requestedQuery
}

func configureTestAPI(t *testing.T, endpoint string) {
	t.Helper()
	viper.Set("api_key", "secret")
	viper.Set("access_token", "")
	viper.Set("api_endpoint", "")
	viper.Set("api_base_url", endpoint)
	t.Cleanup(func() {
		viper.Set("api_key", "")
		viper.Set("access_token", "")
		viper.Set("api_endpoint", "")
		viper.Set("api_base_url", "")
	})
}
