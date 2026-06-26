package ops

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		"database",
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

func TestDefaultTableColumnsOmitOrgID(t *testing.T) {
	for name, columns := range map[string][]string{
		"project":     projectColumns,
		"env":         envColumns,
		"database":    databaseColumns,
		"cluster":     clusterColumns,
		"integration": integrationColumns,
		"provider":    providerColumns,
		"stack":       stackColumns,
		"service":     catalogServiceColumns,
		"app":         appColumns,
		"task":        taskColumns,
	} {
		for _, column := range columns {
			if column == "orgId" {
				t.Fatalf("%s columns should not include orgId", name)
			}
		}
	}
}

func TestDefaultTableColumnsUseReadableRelations(t *testing.T) {
	for name, columns := range map[string][]string{
		"database":   databaseColumns,
		"instance":   instanceColumns,
		"route":      routeColumns,
		"build":      buildColumns,
		"deployment": deploymentColumns,
		"backup":     backupColumns,
		"import":     importColumns,
		"task":       taskColumns,
		"operation":  operationColumns,
	} {
		for _, column := range columns {
			switch column {
			case "envId", "appId", "clusterId", "mainDomain", "appServiceId", "portId", "appInstanceId", "databaseId", "databaseDbId", "taskId":
				t.Fatalf("%s columns should use readable relation names, got %q", name, column)
			}
		}
	}
}

func TestTableColumnsShowIntegrationTitleWithProviderTitle(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":   1,
			"name": "main",
			"integration": map[string]interface{}{
				"title": "Production AWS",
				"provider": map[string]interface{}{
					"title": "Amazon Web Services",
				},
			},
		},
	}, databaseColumns)

	output := out.String()
	if strings.Contains(output, "integrationId") {
		t.Fatalf("output should not include integrationId column: %s", output)
	}
	if !strings.Contains(output, "integration") {
		t.Fatalf("output should include integration column: %s", output)
	}
	if !strings.Contains(output, "Production AWS (Amazon Web Services)") {
		t.Fatalf("output should include integration and provider titles: %s", output)
	}
}

func TestTableColumnsShowProviderTitleWithVersionAndRev(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":   1,
			"name": "aws-prod",
			"providerRev": map[string]interface{}{
				"version": "2.1.0",
				"number":  7,
				"provider": map[string]interface{}{
					"title": "Amazon Web Services",
				},
			},
		},
	}, integrationColumns)

	output := out.String()
	if strings.Contains(output, "providerRevId") || strings.Contains(output, "providerId") {
		t.Fatalf("output should not include provider ID columns: %s", output)
	}
	if !strings.Contains(output, "provider") {
		t.Fatalf("output should include provider column: %s", output)
	}
	if !strings.Contains(output, "Amazon Web Services (2.1.0 #7)") {
		t.Fatalf("output should include provider title, version, and revision: %s", output)
	}
}

func TestTableColumnsShowAppInstanceRelations(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	printTable(cmd, []interface{}{
		map[string]interface{}{
			"id":         1,
			"name":       "prod",
			"title":      "Production",
			"status":     "running",
			"mainDomain": "example.com",
			"app": map[string]interface{}{
				"id":    11,
				"title": "Drupal",
			},
			"env": map[string]interface{}{
				"id":    22,
				"title": "Prod",
			},
			"cluster": map[string]interface{}{
				"id":    33,
				"title": "Primary",
			},
		},
	}, instanceColumns)

	output := out.String()
	for _, rawColumn := range []string{"appId", "envId", "clusterId", "mainDomain"} {
		if strings.Contains(output, rawColumn) {
			t.Fatalf("output should not include %s column: %s", rawColumn, output)
		}
	}
	for _, expected := range []string{"app", "env", "cluster", "domain", "Drupal", "Prod", "Primary", "example.com"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output should include %q: %s", expected, output)
		}
	}
}

func TestGetResultUsesVerticalTableWithRelationIDs(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := printGetResult(cmd, outputOptions{output: outputTable}, map[string]interface{}{
		"id":         1,
		"name":       "prod",
		"title":      "Production",
		"status":     "running",
		"mainDomain": "example.com",
		"app": map[string]interface{}{
			"id":    11,
			"title": "Drupal",
		},
		"env": map[string]interface{}{
			"id":    22,
			"title": "Prod",
		},
	}, instanceColumns)
	if err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, expected := range []string{"id:", "name:", "app:", "Drupal (id: 11)", "app id:", "11", "env:", "Prod (id: 22)", "env id:", "22", "domain:", "example.com"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("vertical output should include %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "appId:") || strings.Contains(output, "envId:") || strings.Contains(output, "mainDomain:") {
		t.Fatalf("vertical output should use readable keys: %s", output)
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

func TestDatabaseCommandExposesBasicOperations(t *testing.T) {
	database := newDatabaseCommand()
	names := make(map[string]bool)
	for _, cmd := range database.Commands() {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "get", "create", "update", "delete"} {
		if !names[name] {
			t.Fatalf("missing database subcommand %q", name)
		}
	}
}

func TestDatabaseCreateUsesPublicAPIShape(t *testing.T) {
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
			"id":    101,
			"name":  "main",
			"title": "Main",
			"type":  "postgres",
		})
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	cmd := newDatabaseCommand()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{
		"create",
		"--env", "3",
		"--integration-kind", "7",
		"--name", "main",
		"--title", "Main",
		"--type", "postgres",
		"--version", "16",
		"--machine-type", "db-s",
		"--org", "10",
		"--project", "12",
		"--region", "us",
		"--zone", "us-a",
		"--resided-cluster", "99",
		"--high-availability",
		"--storage-autoscaling",
		"--storage-size", "50",
		"--iops", "3000",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if requestedMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", requestedMethod)
	}
	if requestedPath != "/v1/databases" {
		t.Fatalf("path = %q, want /v1/databases", requestedPath)
	}
	if body["envId"] != float64(3) {
		t.Fatalf("envId = %#v, want 3", body["envId"])
	}
	if body["integrationKindId"] != float64(7) {
		t.Fatalf("integrationKindId = %#v, want 7", body["integrationKindId"])
	}
	if body["orgId"] != float64(10) {
		t.Fatalf("orgId = %#v, want 10", body["orgId"])
	}
	if body["projectId"] != float64(12) {
		t.Fatalf("projectId = %#v, want 12", body["projectId"])
	}
	if body["residedClusterId"] != float64(99) {
		t.Fatalf("residedClusterId = %#v, want 99", body["residedClusterId"])
	}
	if body["type"] != "postgres" {
		t.Fatalf("type = %#v, want postgres", body["type"])
	}
	if body["version"] != "16" {
		t.Fatalf("version = %#v, want 16", body["version"])
	}
	if body["machineType"] != "db-s" {
		t.Fatalf("machineType = %#v, want db-s", body["machineType"])
	}
	if body["highAvailability"] != true {
		t.Fatalf("highAvailability = %#v, want true", body["highAvailability"])
	}
	if body["storageAutoscaling"] != true {
		t.Fatalf("storageAutoscaling = %#v, want true", body["storageAutoscaling"])
	}
	if body["storageSize"] != float64(50) {
		t.Fatalf("storageSize = %#v, want 50", body["storageSize"])
	}
	if body["iops"] != float64(3000) {
		t.Fatalf("iops = %#v, want 3000", body["iops"])
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
