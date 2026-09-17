package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolveIntegrationUsesCanonicalEnvironmentTypes(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/integrations/actions/resolve" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Fields []struct {
				EnvType *string `json:"envType"`
			} `json:"fieldsInput"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Fields) != 6 || body.Fields[5].EnvType != nil {
			t.Fatalf("fields = %+v", body.Fields)
		}
		for i, want := range []string{"prod", "staging", "test", "dev", "feature"} {
			if body.Fields[i].EnvType == nil || *body.Fields[i].EnvType != want {
				t.Fatalf("field %d scope = %v, want %s", i, body.Fields[i].EnvType, want)
			}
		}
		// Literal REST shape keeps the request test independent of client DTOs.
		_, _ = w.Write([]byte(`{"integration":{"id":9,"orgId":1,"providerRevId":7},"created":true}`))
	}))
	defer server.Close()
	client := mustTargetExecutionClient(t, server.URL)
	input := TargetResolveIntegrationInput{OrgID: 1, ProviderID: 2, Name: "mail", Title: "Mail", Kinds: []string{"smtp"}}
	for _, envType := range []string{"PROD", "Staging", "test", " DEV ", "FEATURE"} {
		input.FieldsInput = append(input.FieldsInput, TargetIntegrationFieldInput{Name: "username", Value: "value", EnvType: stringPointer(envType)})
	}
	input.FieldsInput = append(input.FieldsInput, TargetIntegrationFieldInput{Name: "host", Value: "smtp.example.test"})
	before, _ := json.Marshal(input)
	if _, err := client.ResolveIntegration(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("REST normalization mutated reviewed integration fields")
	}
	input.FieldsInput[0].EnvType = stringPointer("production")
	if _, err := client.ResolveIntegration(context.Background(), input); err == nil || requests != 1 {
		t.Fatalf("invalid type must fail before HTTP: requests=%d err=%v", requests, err)
	}
}

func TestMigrationWaitsForPostDeploymentOutcome(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []string
		wantErr  bool
	}{
		{name: "scripts finish after rollout", statuses: []string{"pending", "in_progress", "completed"}},
		{name: "no scripts", statuses: []string{"not_applicable"}},
		{name: "intentionally skipped", statuses: []string{"skipped"}},
		{name: "failed scripts", statuses: []string{"failed"}, wantErr: true},
		{name: "canceled scripts", statuses: []string{"canceled"}, wantErr: true},
		{name: "scripts not run", statuses: []string{"not_run"}, wantErr: true},
		{name: "unknown outcome", statuses: []string{"unknown"}, wantErr: true},
		{name: "missing outcome", statuses: []string{""}, wantErr: true},
		{name: "future outcome", statuses: []string{"other"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/app-deployments/30" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				index := min(reads, len(test.statuses)-1)
				reads++
				writeTargetExecutionJSON(t, w, TargetAppDeployment{
					ID: 30, AppInstanceID: 20, Status: "completed", PostDeploymentStatus: test.statuses[index],
				})
			}))
			defer server.Close()
			executor := newAsyncTestExecutor(t, server.URL, t.TempDir()+"/state.json", time.Second)
			_, err := executor.waitDeployment(context.Background(), 30)
			if (err != nil) != test.wantErr || reads != len(test.statuses) {
				t.Fatalf("reads=%d err=%v", reads, err)
			}
		})
	}
}

func TestPrepareStackConfigurationBlocksEmptyCustomValues(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		dev := stackConfigurationTestInstance("dev", "DEV", "development", "shared")
		prod := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
		dev.Source.Services[0].EnvVars = append(dev.Source.Services[0].EnvVars, EnvVar{Name: "CUSTOM_EMPTY", Enabled: true})
		if !scoped {
			prod.Source.Services[0].EnvVars = append(prod.Source.Services[0].EnvVars, EnvVar{Name: "CUSTOM_EMPTY", Enabled: true})
		}
		_, findings, err := prepareStackConfigurationTest(stackConfigurationTestApp(dev, prod))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, finding := range findings {
			found = found || finding.Severity == SeverityBlocking && finding.Instance == "dev" &&
				strings.Contains(finding.Subject, "php env var CUSTOM_EMPTY") && strings.Contains(finding.Message, "empty")
		}
		if !found {
			t.Fatalf("scoped=%t findings=%+v", scoped, findings)
		}
	}
}

func TestResumeRechecksPreviouslySuccessfulDeploymentScripts(t *testing.T) {
	for _, status := range []string{"failed", "completed"} {
		t.Run(status, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/app-deployments/30" {
					t.Fatalf("resume must read the recorded deployment, not create another: %s %s", r.Method, r.URL.Path)
				}
				reads++
				deployment := asyncTestDeployment(30, 20, 40, "completed", time.Now())
				deployment.PostDeploymentStatus = status
				writeTargetExecutionJSON(t, w, deployment)
			}))
			defer server.Close()
			state, path := newExecutorTestState(t)
			operation := operationKey("prepare_deploy", "instance-1")
			if err := state.MarkInstanceOperationIntent("instance-1", operation); err != nil {
				t.Fatal(err)
			}
			if err := state.MarkInstanceOperationSuccessWithIDs("instance-1", operation, 30, 40); err != nil {
				t.Fatal(err)
			}
			executor := newAsyncTestExecutor(t, server.URL, path, time.Second)
			err := executor.ensureDeployment(context.Background(), state, "instance-1", 20, operation,
				TargetCreateAppDeploymentInput{Services: []TargetAppServiceDeploymentInput{{AppServiceID: 10, Force: true}}})
			if (err != nil) != (status == "failed") || reads != 1 {
				t.Fatalf("reads=%d err=%v", reads, err)
			}
		})
	}
}

func TestPlanChecksMappedEnvironmentAgainstClusterPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		scope   string
		allowed []string
		mapping map[string]string
		blocked bool
		want    string
	}{
		{name: "all", scope: "all", want: "PROD"},
		{name: "legacy policy absent", want: "PROD"},
		{name: "selected allowed", scope: "selected", allowed: []string{"prod"}, want: "PROD"},
		{name: "selected disallowed", scope: "selected", allowed: []string{"dev"}, blocked: true, want: "PROD"},
		{name: "empty allowlist", scope: "selected", blocked: true, want: "PROD"},
		{name: "unknown scope", scope: "other", blocked: true, want: "PROD"},
		{name: "unknown type", scope: "selected", allowed: []string{"prod", "other"}, blocked: true, want: "PROD"},
		{name: "explicit remapping", scope: "selected", allowed: []string{"staging"}, mapping: map[string]string{"prod": "staging"}, want: "STAGING"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := Plan{}
			opts := PlanOptions{TargetEnvMap: test.mapping, TargetScope: &TargetScopeDiscovery{
				Org: TargetOrg{ID: 1}, Cluster: TargetCluster{Name: "destination", EnvScope: test.scope, AllowedEnvTypes: test.allowed},
			}, TargetEnvs: targetEnvironmentTypes(1)}
			instance := buildInstancePlan(&plan, App{Name: "app"}, Instance{UUID: "source", Name: "live", Type: "prod"}, opts, false)
			blocked := false
			for _, item := range plan.Review {
				if item.Subject == "target cluster environment type" && item.Severity == SeverityBlocking {
					blocked = true
				}
				if item.Subject == "target env type" {
					t.Fatalf("destination type incorrectly checked against source: %+v", item)
				}
			}
			if blocked != test.blocked || instance.TargetEnvType != test.want {
				t.Fatalf("blocked=%t type=%q review=%+v", blocked, instance.TargetEnvType, plan.Review)
			}
		})
	}
}

func TestTargetEnvironmentMappingRejectsCatalogSelectors(t *testing.T) {
	for _, invalid := range []string{"1", "production-eu", "production", ""} {
		if _, _, ok := resolveTargetEnv("prod", map[string]string{"prod": invalid}); ok {
			t.Fatalf("accepted noncanonical destination type %q", invalid)
		}
	}
}

func TestClusterPolicyReadbackUsesTypesRatherThanLegacyIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/clusters/30" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// Physical environment IDs are organization-specific. Eligibility comes
		// from the type list, never from synthetic migration-plan identifiers.
		_, _ = w.Write([]byte(`{"id":30,"orgId":1,"name":"destination","envScope":"selected","allowedEnvIds":[901],"allowedEnvTypes":["staging"]}`))
	}))
	defer server.Close()
	cluster, err := mustTargetExecutionClient(t, server.URL).GetCluster(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTargetClusterEnvironment(cluster, "STAGING"); err != nil {
		t.Fatal(err)
	}
	if err := validateTargetClusterEnvironment(cluster, "PROD"); err == nil {
		t.Fatal("readback lost the cluster environment restriction")
	}
}

func TestClusterPolicyIgnoresContextOnlySiblings(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2, Source: &ExportSource{Kind: "instance", UUID: "selected"},
		Apps: []AppExport{{
			App:              App{UUID: "app", Name: "app", Status: "ok"},
			Instances:        []Instance{{UUID: "selected", Name: "live", Type: "prod", Status: "ok", Stack: Stack{Name: "wordpress"}}},
			ContextInstances: []Instance{{UUID: "context", Name: "development", Type: "dev", Status: "ok", Stack: Stack{Name: "wordpress"}}},
		}},
	}
	opts := PlanOptions{SourceKind: "instance", SourceID: "selected", TargetScope: &TargetScopeDiscovery{
		Org: TargetOrg{ID: 1}, Cluster: TargetCluster{ID: 30, OrgID: 1, Name: "destination", Status: "OK", EnvScope: "selected", AllowedEnvTypes: []string{"prod"}},
	}, TargetEnvs: targetEnvironmentTypes(1), SkipData: true}
	plan, err := BuildPlan(export, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range plan.Review {
		if finding.Subject == "target cluster environment type" {
			t.Fatalf("context-only environment blocked the selected migration: %+v", finding)
		}
	}
	// Rebuilding the same selection after a policy change must now block it.
	opts.TargetScope.Cluster.AllowedEnvTypes = []string{"dev"}
	plan, err = BuildPlan(export, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range plan.Review {
		if finding.Subject == "target cluster environment type" && finding.Instance == "live" && finding.Severity == SeverityBlocking {
			return
		}
	}
	t.Fatal("changed cluster policy did not block reconstruction")
}

func TestRouteReadbackPreservesTimeoutAndRateLimitOverrides(t *testing.T) {
	const body = `[
		{"id":1,"appInstanceId":20,"routeId":30,"name":"REQUEST_TIMEOUT","value":"60s"},
		{"id":2,"appInstanceId":20,"routeId":30,"name":"BACKEND_REQUEST_TIMEOUT","value":"30s"},
		{"id":3,"appInstanceId":20,"routeId":30,"name":"RATE_LIMIT_PER_IP","value":"60/minute"},
		{"id":4,"appInstanceId":20,"routeId":30,"name":"RATE_LIMIT_TOTAL","value":"1000/second"},
		{"id":5,"appInstanceId":20,"routeId":30,"name":"HSTS","value":"enabled"}
	]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/app-routes/30/settings" {
			t.Fatalf("unrelated route settings must not be mutated: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	state, path := newExecutorTestState(t)
	executor := newAsyncTestExecutor(t, server.URL, path, time.Second)
	if err := executor.ensureRouteSetting(context.Background(), state, "instance-1", 30, RouteSettingPlan{Name: "HSTS", Value: "enabled"}); err != nil {
		t.Fatal(err)
	}
	settings, err := executor.target.ListAppRouteSettings(context.Background(), 30)
	var want []TargetAppRouteSetting
	_ = json.Unmarshal([]byte(body), &want)
	if err != nil || !reflect.DeepEqual(settings, want) {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}
