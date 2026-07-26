package migrate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

var (
	testSourceEnvToken  = strings.Repeat("b", 64)
	testSourceFlagToken = strings.Repeat("c", 64)
)

func TestWodby1AppCommandUsesEnvTokenAndWritesReadOnlyPlan(t *testing.T) {
	var gotPath string
	var gotAPIKey string
	var gotAuthorization string
	port := 80
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-API-Key")
		gotAuthorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchemaV2,
			Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
			Apps: []wodby1.AppExport{{
				App: wodby1.App{UUID: "app-1", Name: "demo", Title: "Demo"},
				Instances: []wodby1.Instance{{
					UUID:  "inst-1",
					Name:  "prod",
					Type:  "prod",
					Stack: wodby1.Stack{Name: "drupal10"},
					Domains: []wodby1.Domain{{
						UUID:       "domain-1",
						Name:       "example.com",
						Type:       "user",
						Primary:    true,
						Service:    "nginx",
						PortNumber: &port,
					}},
				}},
			}},
		})
	}))
	defer server.Close()
	t.Setenv(sourceTokenEnv, testSourceEnvToken)

	planPath := filepath.Join(t.TempDir(), "plan.json")
	cmd := newWodby1AppCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--target-env-map", "prod=production",
		"--plan-file", planPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v4/migrations/v2/apps/app-1/export" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAPIKey != testSourceEnvToken || gotAuthorization != "" {
		t.Fatalf("auth headers: X-API-Key=%q Authorization=%q", gotAPIKey, gotAuthorization)
	}
	if !strings.Contains(out.String(), "Wodby 1 migration inventory (read-only)") {
		t.Fatalf("output = %s", out.String())
	}

	content, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("plan mode = %o, want 600", info.Mode().Perm())
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(content, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Apps[0].Instances[0].TargetEnv != "production" {
		t.Fatalf("plan env = %#v", plan.Apps[0].Instances[0])
	}
}

func TestWodby1CommandDoesNotExposeExecutionFlags(t *testing.T) {
	cmd := newWodby1AppCommand()
	for _, name := range []string{
		"execute",
		"dry-run",
		"create-source-backup",
		"create-missing-envs",
		"stack-map",
		"service-map",
		"state-file",
		"resume",
		"parallel",
		"continue-on-error",
		"yes",
		"accept-review",
		"assume-envoy-gateway",
	} {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			t.Errorf("unexpected no-op flag --%s", name)
		}
	}
}

func TestWodby1CommandWritesJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchemaV2,
			Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
			Apps: []wodby1.AppExport{{
				App: wodby1.App{UUID: "app-1", Name: "demo"},
				Instances: []wodby1.Instance{{
					UUID:  "inst-1",
					Name:  "prod",
					Type:  "prod",
					Stack: wodby1.Stack{Name: "drupal10"},
				}},
			}},
		})
	}))
	defer server.Close()

	cmd := newWodby1AppCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--source-token", testSourceFlagToken,
		"--output", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Schema != "wodby1-migration-plan/v2" {
		t.Fatalf("plan schema = %q", plan.Schema)
	}
}

func TestWodby1CommandSourceTokenFlagOverridesEnvironment(t *testing.T) {
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchemaV2,
			Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
			Apps: []wodby1.AppExport{{
				App: wodby1.App{UUID: "app-1", Name: "demo"},
			}},
		})
	}))
	defer server.Close()
	t.Setenv(sourceTokenEnv, testSourceEnvToken)

	cmd := newWodby1AppCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--source-token", testSourceFlagToken,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != testSourceFlagToken {
		t.Fatalf("X-API-Key = %q", gotAPIKey)
	}
}

func TestWodby1CommandRequiresTargetAdministrator(t *testing.T) {
	sourceCalled := false
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalled = true
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchemaV2,
			Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
			Apps:   []wodby1.AppExport{},
		})
	}))
	defer sourceServer.Close()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(wodby1.TargetCurrentUser{
			ID:      7,
			Email:   "member@example.com",
			IsAdmin: false,
		})
	}))
	defer targetServer.Close()

	viper.Set("api_base_url", targetServer.URL+"/v1")
	viper.Set("api_key", "target-key")
	viper.Set("access_token", "")
	t.Cleanup(func() {
		viper.Set("api_base_url", "")
		viper.Set("api_key", "")
		viper.Set("access_token", "")
	})

	cmd := newWodby1AppCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", sourceServer.URL,
		"--source-token", testSourceFlagToken,
		"--target-org", "org",
		"--target-project", "project",
		"--target-cluster", "cluster",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "administrator credentials are required") {
		t.Fatalf("err = %v", err)
	}
	if sourceCalled {
		t.Fatal("source export must not be requested before target administrator verification")
	}
}

func TestWodby1CommandRequiresCompleteTargetSelector(t *testing.T) {
	cmd := newWodby1AppCommand()
	cmd.SetArgs([]string{
		"app-1",
		"--source-token", testSourceFlagToken,
		"--target-org", "org",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "must be specified together") {
		t.Fatalf("err = %v", err)
	}
}

func TestWodby1CommandDiscoversExactTargetScopeAndEnvironment(t *testing.T) {
	sourceCalled := false
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalled = true
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema: wodby1.ExportSchemaV2,
			Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
			Apps: []wodby1.AppExport{{
				App: wodby1.App{UUID: "app-1", Name: "demo", Status: "ok"},
				Instances: []wodby1.Instance{{
					UUID:   "inst-1",
					Name:   "prod",
					Type:   "prod",
					Status: "ok",
					Stack:  wodby1.Stack{Name: "drupal10"},
				}},
			}},
		})
	}))
	defer sourceServer.Close()

	var targetPaths []string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetPaths = append(targetPaths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/user":
			_ = json.NewEncoder(w).Encode(wodby1.TargetCurrentUser{ID: 7, IsAdmin: true})
		case "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]wodby1.TargetOrg{{ID: 11, Name: "acme"}})
		case "/v1/projects":
			_ = json.NewEncoder(w).Encode([]wodby1.TargetProject{{ID: 22, Name: "site", OrgID: 11}})
		case "/v1/clusters":
			_ = json.NewEncoder(w).Encode([]wodby1.TargetCluster{{
				ID:     33,
				Name:   "primary",
				Status: "OK",
				OrgID:  11,
				Capabilities: wodby1.TargetClusterCapabilities{
					EnvoyGateway:   true,
					RedirectRoutes: true,
				},
			}})
		case "/v1/envs":
			if !sourceCalled {
				t.Fatal("environment discovery must happen after the source export identifies required environments")
			}
			_ = json.NewEncoder(w).Encode([]wodby1.TargetEnv{{ID: 44, Name: "production", Type: "PROD", OrgID: 11}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer targetServer.Close()

	viper.Set("api_base_url", targetServer.URL+"/v1")
	viper.Set("api_key", "target-key")
	viper.Set("access_token", "")
	t.Cleanup(func() {
		viper.Set("api_base_url", "")
		viper.Set("api_key", "")
		viper.Set("access_token", "")
	})

	cmd := newWodby1AppCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", sourceServer.URL,
		"--source-token", testSourceFlagToken,
		"--target-org", "acme",
		"--target-project", "site",
		"--target-cluster", "primary",
		"--target-env-map", "prod=production",
		"--output", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var plan wodby1.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Target.AdminVerified || !plan.Target.DiscoveryVerified ||
		plan.Target.OrgID != 11 || plan.Target.ProjectID != 22 || plan.Target.ClusterID != 33 {
		t.Fatalf("target = %#v", plan.Target)
	}
	if plan.Apps[0].Instances[0].TargetEnvID != 44 {
		t.Fatalf("instance = %#v", plan.Apps[0].Instances[0])
	}
	expectedPaths := []string{
		"/v1/user",
		"/v1/orgs",
		"/v1/projects?orgId=11",
		"/v1/clusters?orgId=11",
		"/v1/clusters?orgId=11&projectIds=22",
		"/v1/envs?orgId=11",
	}
	if strings.Join(targetPaths, "\n") != strings.Join(expectedPaths, "\n") {
		t.Fatalf("target requests = %#v", targetPaths)
	}
	if plan.Status != "target_scope_validated" || plan.Summary.Blocking != 0 {
		t.Fatalf("status = %q, summary = %#v", plan.Status, plan.Summary)
	}
}

func TestWodby1CommandRejectsInvalidMappingBeforeNetwork(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := newWodby1AppCommand()
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", server.URL,
		"--source-token", testSourceFlagToken,
		"--target-env-map", "invalid",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "source=target") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("invalid local options must be rejected before any network request")
	}
}

func TestParseMappingRejectsConflictingSourceMappings(t *testing.T) {
	_, err := parseMapping([]string{"prod=production", "prod=other"}, "--target-env-map")
	if err == nil || !strings.Contains(err.Error(), "conflicting mappings") {
		t.Fatalf("err = %v", err)
	}
}
