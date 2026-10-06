package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A resumed run finds the first variable already applied. The second one must
// still be created instead of the step ending there.
func TestEnsureStackEnvVarsContinuesAfterAnAppliedItem(t *testing.T) {
	first := "one"
	created := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/stack-revisions/12/env-vars":
			writeTargetExecutionJSON(t, w, []TargetStackEnvVar{{ID: 40, Name: "FIRST", Value: &first}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/stacks/5/env-vars":
			var body TargetCreateStackEnvVarInput
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			created = append(created, body.Name)
			writeTargetExecutionJSON(t, w, TargetStackEnvVar{ID: 41, Name: body.Name, Value: &body.Value})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), statePath: statePath}
	err := executor.ensureStackEnvVars(context.Background(), state, 5, 12, []PreparedStackEnvVar{
		{Name: "FIRST", Value: "one"}, {Name: "SECOND", Value: "two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0] != "SECOND" {
		t.Fatalf("created variables = %v, want the one that was missing", created)
	}
}

// Wodby 2 removes a setting override set to an empty value, so a Drupal root at
// the repository root is stored as ".".
func TestPrepareStackConfigurationMapsRootDocroot(t *testing.T) {
	instance := stackConfigurationTestInstance("prod", "PROD", "production", "shared")
	instance.Source.Stack = Stack{Name: "drupal11"}
	instance.EffectiveState = map[string]bool{"php": true}
	instance.StackServices = []TargetStackServiceInspection{instance.Services["php"].Target}
	app := stackConfigurationTestApp(instance)
	app.App.App.Docroot = stringPointer("")
	app.App.App.SiteName = stringPointer("default")

	configuration, findings, err := prepareStackConfigurationTest(app)
	if err != nil || hasBlockingFindings(findings) {
		t.Fatalf("err = %v, findings = %#v", err, findings)
	}
	if got := configuration.Services["php"].Settings["docroot"]; got != "." {
		t.Fatalf("docroot setting = %q, want %q", got, ".")
	}
}

func TestDomainRedirects(t *testing.T) {
	for _, tc := range []struct {
		domain Domain
		want   bool
	}{
		{Domain{Name: "example.com", RedirectToWWW: true}, true},
		{Domain{Name: "www.example.com", RedirectNonWWW: true}, true},
		// Wodby 1 ignores an option that points a host at itself.
		{Domain{Name: "www.example.com", RedirectToWWW: true}, false},
		{Domain{Name: "app.example.com", RedirectNonWWW: true}, false},
		{Domain{Name: "old.example.com", RedirectTarget: "new.example.com"}, true},
		{Domain{Name: "example.com"}, false},
	} {
		if got := domainRedirects(tc.domain); got != tc.want {
			t.Errorf("domainRedirects(%+v) = %v, want %v", tc.domain, got, tc.want)
		}
	}
}

// A host redirect keeps the request path; only a target that names a path
// replaces it.
func TestRouteRedirectTargetKeepsRequestPath(t *testing.T) {
	_, host, path, err := routeRedirectTarget(RoutePlan{Host: "example.com", SSL: true, RedirectToWWW: true})
	if err != nil || host != "www.example.com" || path != "" {
		t.Fatalf("www redirect = %q %q, %v", host, path, err)
	}
	_, host, path, err = routeRedirectTarget(RoutePlan{Host: "old.example.com", RedirectTarget: "https://new.example.com/shop"})
	if err != nil || host != "new.example.com" || path != "/shop" {
		t.Fatalf("explicit redirect = %q %q, %v", host, path, err)
	}
}

func TestGeneratedStackMayBelongTo(t *testing.T) {
	blueprint := TargetStack{Name: "drupal11", Title: "Drupal 11"}
	app := App{UUID: "app-1", Name: "shop", Title: "Shop"}
	other := App{UUID: "app-2", Name: "blog", Title: "Blog"}
	for name, want := range map[string]bool{
		"drupal11":   true,
		"drupal11-2": true,
		generatedStackNaming(blueprint, app).Name:   true,
		generatedStackNaming(blueprint, other).Name: false,
		"drupal11-custom":                           false,
		"wordpress":                                 false,
	} {
		if got := generatedStackMayBelongTo(TargetStack{Name: name}, blueprint, app); got != want {
			t.Errorf("stack %q: candidate = %v, want %v", name, got, want)
		}
	}
}

func TestBuildPlanBlocksMappingKeysThatMatchNothing(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
	options := preflightOwnerPlanOptions()
	options.TargetVersionMap = map[string]string{"php": "8.3", "exmaple/prod/php": "8.3"}
	options.TargetServiceMap = map[string]string{"example/prod/php": "php"}
	options.TargetEnvMap = map[string]string{"stagin": "staging"}
	plan := preflightBuildPlan(t, export, options)

	blocked, warned := []string{}, false
	for _, item := range plan.Review {
		if item.Severity == SeverityBlocking && strings.HasPrefix(item.Subject, "--target-") {
			blocked = append(blocked, item.Subject+" "+item.Message)
		}
		warned = warned || (item.Subject == "--target-env-map" && item.Severity == SeverityServiceWarning)
	}
	if len(blocked) != 1 || !strings.Contains(blocked[0], `"exmaple/prod/php"`) {
		t.Fatalf("blocked mapping keys = %q", blocked)
	}
	if !warned {
		t.Fatal("an environment mapping for an absent instance type must be reported")
	}
}

// The second preflight runs against a pinned plan and must still read the
// reviewed stack revision's manifest, which holds the stack's version options.
func TestResolvePreflightStackRevisionKeepsPinnedCatalogManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/stacks/7":
			writeTargetExecutionJSON(t, w, TargetStack{ID: 7, Name: "drupal11", Status: "OK", Public: true, RevID: 71, LatestRevNumber: 4, OrgID: 1})
		case "/v1/stack-revisions/71":
			writeTargetExecutionJSON(t, w, TargetStackRevision{ID: 71, StackID: 7, Number: 4, Manifest: "name: drupal11"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	stack, err := mustTargetExecutionClient(t, server.URL).resolvePreflightStackRevision(context.Background(), 8, 0, StackPlan{
		CreateTarget: true, CatalogName: "drupal11", TargetID: 7, TargetRevID: 71,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stack.RevisionManifest != "name: drupal11" {
		t.Fatalf("pinned catalog stack manifest = %q", stack.RevisionManifest)
	}
}

// A sibling that is only read for context can be deployed or put into
// maintenance while a migration is paused. That must not change the digest.
func TestConfigDigestIgnoresContextInstanceActivity(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].ContextInstances = []Instance{{
		UUID: "inst-2", Name: "dev", Type: "dev", Status: "ok", Updated: 40,
		Stack: Stack{UUID: "stack-1", Name: "drupal11"}, Properties: map[string]interface{}{"maintenance_mode": false},
	}}
	before, err := export.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	export.Apps[0].ContextInstances[0].Updated = 99
	export.Apps[0].ContextInstances[0].Properties["maintenance_mode"] = true
	after, err := export.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("context instance activity changed the configuration digest")
	}
}

func TestRollbackPlanWaitsForTeardownAndReportsWhatStays(t *testing.T) {
	state, _ := newExecutorTestState(t)
	state.App.TargetID = 10
	for _, instance := range state.Instances {
		instance.TargetID = 20
	}
	if err := state.MarkAppOperationIntent(generatedStackOperation); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationCreated(generatedStackOperation, 5, 12); err != nil {
		t.Fatal(err)
	}
	provider := operationKey("variable_provider", "shared")
	if err := state.MarkAppOperationIntent(provider); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs(provider, 77, 78); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRollback(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AppID != 10 || plan.StackID != 5 || len(plan.TeardownInstanceIDs) == 0 || plan.TeardownInstanceIDs[0] != 20 {
		t.Fatalf("rollback plan = %#v", plan)
	}
	if plan.ChangedExistingStack {
		t.Fatal("a stack the migration generated is deleted, not reported as changed")
	}
	if description := plan.Describe("demo"); !strings.Contains(description, "custom variable provider ID(s) [77]") {
		t.Fatalf("rollback description does not name what stays:\n%s", description)
	}
}

// A failed verification leaves the phase at verify. Apply must be able to
// repeat its steps from there, and the phase must stay so rollback remains
// refused once DNS has moved.
func TestStartPhaseAllowsRepairAfterFailedVerification(t *testing.T) {
	state, statePath := newExecutorTestState(t)
	executor := &MigrationExecutor{statePath: statePath}
	if err := executor.startPhase(state, MigrationPhaseVerify); err != nil {
		t.Fatal(err)
	}
	if err := executor.startPhase(state, MigrationPhasePrepare); err != nil {
		t.Fatalf("repair after failed verification: %v", err)
	}
	if state.Phase != MigrationPhaseVerify {
		t.Fatalf("phase = %q, want verify to stay", state.Phase)
	}
	if _, err := PlanRollback(state); err != ErrRollbackAfterCutover {
		t.Fatalf("rollback after cutover = %v", err)
	}
}
