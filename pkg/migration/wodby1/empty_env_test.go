package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func emptyEnvTestInspection(t *testing.T, stackServiceName string, manifest map[string]interface{}) TargetStackServiceInspection {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return TargetStackServiceInspection{
		StackService:    TargetStackService{Name: stackServiceName},
		ServiceRevision: TargetServiceRevision{Name: "drupal-nginx", Manifest: &TargetServiceManifest{Name: "drupal-nginx", Raw: string(raw)}},
	}
}

// A value the target gives a variable is found wherever a manifest can set it:
// an env list at any depth, or a setting's default. Empty values set nothing.
func TestTargetEnvVarDefaultFindsManifestValues(t *testing.T) {
	inspection := emptyEnvTestInspection(t, "nginx", map[string]interface{}{
		"name": "drupal-nginx",
		"env":  []interface{}{map[string]interface{}{"name": "NGINX_EMPTY", "value": ""}},
		"workloads": []interface{}{map[string]interface{}{"name": "nginx", "containers": []interface{}{map[string]interface{}{
			"name": "nginx",
			"env":  []interface{}{map[string]interface{}{"name": "NGINX_CONTAINER", "value": "on"}},
		}}}},
		"settings": []interface{}{
			map[string]interface{}{"name": "token", "var": "NGINX_TOKEN", "default": "abc", "secret": true},
			map[string]interface{}{"name": "unset", "var": "NGINX_UNSET"},
		},
	})
	stack := `
name: drupal
env:
  - name: STACK_WIDE
    value: "1"
services:
  - name: php
    env:
      - name: PHP_ONLY
        value: "2"
  - name: nginx
    env:
      - name: NGINX_STACK
        value: "3"
`
	for _, tc := range []struct {
		name, value, from string
		secret, found     bool
	}{
		{name: "NGINX_CONTAINER", value: "on", from: `service "drupal-nginx"`, found: true},
		{name: "NGINX_TOKEN", value: "abc", from: `service "drupal-nginx"`, secret: true, found: true},
		{name: "STACK_WIDE", value: "1", from: "stack", found: true},
		{name: "NGINX_STACK", value: "3", from: `stack service "nginx"`, found: true},
		// Another stack service's variable, an empty value and a setting without
		// a default give this service nothing.
		{name: "PHP_ONLY"},
		{name: "NGINX_EMPTY"},
		{name: "NGINX_UNSET"},
		{name: "NGINX_HIDE_50x_ERRORS"},
	} {
		value, secret, from, found := targetEnvVarDefault(inspection, stack, tc.name)
		if value != tc.value || secret != tc.secret || from != tc.from || found != tc.found {
			t.Errorf("%s: got %q secret=%t from=%q found=%t", tc.name, value, secret, from, found)
		}
	}
	if _, _, _, found := targetEnvVarDefault(TargetStackServiceInspection{}, "not: [yaml", "STACK_WIDE"); found {
		t.Fatal("an unreadable manifest gives no value")
	}
}

// An empty Wodby 1 override blocks only when the target sets the variable,
// and then names the value unless it is secret.
func TestEmptyEnvVarBlocksOnlyOverATargetValue(t *testing.T) {
	inspection := emptyEnvTestInspection(t, "nginx", map[string]interface{}{
		"env": []interface{}{
			map[string]interface{}{"name": "NGINX_ON_BY_DEFAULT", "value": "on"},
			map[string]interface{}{"name": "NGINX_KEY", "value": "k", "secret": true},
		},
	})
	source := Instance{Name: "prod"}
	service := Service{Name: "nginx", EnvVars: []EnvVar{
		{Name: "NGINX_HIDE_50x_ERRORS", Enabled: true, Origin: "custom"},
		{Name: "NGINX_ON_BY_DEFAULT", Enabled: true, Origin: "custom"},
		{Name: "NGINX_KEY", Enabled: true, Origin: "custom"},
		// Set, disabled or reserved values are not this rule's.
		{Name: "NGINX_ON_BY_DEFAULT", Value: "off", Enabled: true, Origin: "custom"},
		{Name: "NGINX_ON_BY_DEFAULT", Enabled: false, Origin: "custom"},
	}}
	findings := emptyEnvVarTargetDefaultFindings("shop", source, service, inspection, "")
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	first, second := findings[0], findings[1]
	if first.Severity != SeverityBlocking || first.App != "shop" || first.Instance != "prod" ||
		first.Subject != "service nginx env var NGINX_ON_BY_DEFAULT" || !strings.Contains(first.Message, `sets it to "on"`) ||
		!strings.Contains(first.Message, "remove the override") {
		t.Fatalf("first = %+v", first)
	}
	if second.Subject != "service nginx env var NGINX_KEY" || !strings.Contains(second.Message, "a secret value") || strings.Contains(second.Message, `"k"`) {
		t.Fatalf("second = %+v", second)
	}
}

// The plan notes an empty Wodby 1 value as left unset and does not count it
// as a variable to migrate.
func TestBuildPlanLeavesEmptyEnvironmentValuesUnset(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "inst-1", Name: "prod", Type: "prod", Stack: Stack{Name: "drupal10"},
			Services: []Service{{Name: "nginx", Enabled: true, EnvVars: []EnvVar{
				{Name: "NGINX_HIDE_50x_ERRORS", Enabled: true, Origin: "custom"},
				{Name: "NGINX_SET", Value: "1", Enabled: true, Origin: "custom"},
			}}},
		}},
	}
	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("review = %#v", plan.Review)
	}
	if plan.Apps[0].Instances[0].EnvVars != 1 {
		t.Fatalf("migrated variables = %d", plan.Apps[0].Instances[0].EnvVars)
	}
	if !hasReviewMessage(plan.Review, SeveritySkipped, "NGINX_HIDE_50x_ERRORS is empty") {
		t.Fatalf("review = %#v", plan.Review)
	}
}

// The run never writes an empty value: Wodby 2 would refuse it.
func TestEnsureServiceEnvironmentSkipsEmptyValues(t *testing.T) {
	created := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services/10/env-vars":
			writeTargetExecutionJSON(t, w, []TargetAppServiceEnvVar{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-services/10/env-vars":
			body := decodeTargetExecutionObject(t, r)
			name, _ := body["name"].(string)
			created = append(created, name)
			writeTargetExecutionJSON(t, w, TargetAppServiceEnvVar{ID: 30, AppServiceID: 10, Name: name, Value: "1", Runtime: true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), state: FileStateStore(statePath)}
	err := executor.ensureServiceEnvironment(context.Background(), state, "instance-1", TargetAppService{ID: 10},
		Service{EnvVars: []EnvVar{
			{Name: "NGINX_HIDE_50x_ERRORS", Enabled: true, Origin: "custom"},
			{Name: "NGINX_SET", Value: "1", Enabled: true, Origin: "custom"},
		}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0] != "NGINX_SET" {
		t.Fatalf("created = %q", created)
	}
}

// Through the whole preview: an empty value the target leaves unset is noted
// and the plan stays unblocked; one the target sets blocks the plan.
func TestPreflightTargetBlocksEmptyValueOnlyOverATargetValue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     []interface{}
		blocked bool
	}{
		{name: "target leaves it unset"},
		{name: "target sets it", env: []interface{}{map[string]interface{}{"name": "PHP_HIDE_ERRORS", "value": "1"}}, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			export := preflightFixtureExport(true)
			export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true, EnvVars: []EnvVar{
				{Name: "PHP_HIDE_ERRORS", Enabled: true, Origin: "custom"},
			}}}
			options := preflightOwnerPlanOptions()
			options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
			plan := preflightBuildPlan(t, export, options)
			catalog := preflightOfficialCatalog()
			// The backend sends the whole manifest as raw JSON: the fixture's
			// manifest, with the env list of the case.
			revision := catalog.revisions[101]
			data, err := json.Marshal(revision.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			manifest := map[string]interface{}{}
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			// A Drupal PHP service has these settings, which a service with
			// variables gets checked for.
			manifest["settings"] = []interface{}{
				map[string]interface{}{"name": "docroot", "title": "Docroot", "var": "WODBY_DOCROOT_SUBDIR", "default": "web"},
				map[string]interface{}{"name": "sitedir", "title": "Site directory", "var": "WODBY_SITE_DIR", "default": "default"},
			}
			if tc.env != nil {
				manifest["env"] = tc.env
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			withRaw := *revision.Manifest
			withRaw.Raw = string(raw)
			revision.Manifest = &withRaw
			catalog.revisions[101] = revision
			api := newPreflightTargetAPI(t, catalog)
			if _, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{}); err != nil {
				t.Fatal(err)
			}
			if !hasReviewMessage(plan.Review, SeveritySkipped, "PHP_HIDE_ERRORS is empty") {
				t.Fatalf("review = %#v", plan.Review)
			}
			blocked := hasReviewMessage(plan.Review, SeverityBlocking, `sets it to "1"`)
			if blocked != tc.blocked || (plan.Summary.Blocking != 0) != tc.blocked {
				t.Fatalf("blocked = %t, blocking = %d, review = %#v", blocked, plan.Summary.Blocking, plan.Review)
			}
		})
	}
}
