package wodby1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBuildPlanCapturesManagedMigrationReviewItems(t *testing.T) {
	indexed := false
	sslRequired := true
	port := 80
	export := Export{
		Schema: ExportSchema,
		App:    &App{UUID: "app-1", Name: "demo", Title: "Demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "prod",
			Type:  "prod",
			Stack: Stack{Name: "drupal10"},
			BasicAuth: &BasicAuth{
				Enabled: true,
				Login:   "admin",
				Secret:  true,
			},
			Domains: []Domain{{
				Name:           "example.com",
				Type:           "user",
				Primary:        true,
				Indexed:        &indexed,
				SSLRequired:    &sslRequired,
				Protected:      true,
				Service:        "nginx",
				PortNumber:     &port,
				RedirectToWWW:  true,
				RedirectTarget: "www.example.com",
			}},
			Services: []Service{
				{
					Name:    "php",
					Enabled: true,
					EnvVars: []EnvVar{{
						Name:    "CUSTOM",
						Value:   "value",
						Enabled: true,
						Origin:  "custom",
					}},
					CronJobs: []CronJob{{
						Crontab: "* * * * *",
						Command: "drush cron",
						Enabled: true,
					}},
				},
				{Name: "athenapdf", Enabled: true},
				{Name: "rsyslog", Enabled: true},
			},
			Backups: []Backup{{UUID: "backup-1", Component: "database", URL: "https://example.com/backup.sql", Status: "ok"}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Status != "blocked" {
		t.Fatalf("status = %q", plan.Status)
	}
	if plan.Summary.Apps != 1 || plan.Summary.Instances != 1 || plan.Summary.Routes != 1 {
		t.Fatalf("summary = %#v", plan.Summary)
	}
	if plan.Summary.EnvVars != 1 || plan.Summary.CronJobs != 1 || plan.Summary.Imports != 1 {
		t.Fatalf("summary = %#v", plan.Summary)
	}
	if plan.Summary.Blocking != 2 {
		t.Fatalf("blocking = %d, want basic auth + redirect gateway", plan.Summary.Blocking)
	}
	instance := plan.Apps[0].Instances[0]
	if instance.TargetEnvType != "PROD" || instance.TargetEnv != "prod" {
		t.Fatalf("target env = %s/%s", instance.TargetEnvType, instance.TargetEnv)
	}
	route := instance.Routes[0]
	if !route.NeedsPortID || route.PortNumber == nil || *route.PortNumber != 80 {
		t.Fatalf("route port resolution = %#v", route)
	}
	if len(route.Settings) != 2 ||
		route.Settings[0] != (RouteSettingPlan{Name: "HTTPS_REDIRECT", Value: "true"}) ||
		route.Settings[1] != (RouteSettingPlan{Name: "NO_INDEX", Value: "true"}) {
		t.Fatalf("route settings = %#v", route.Settings)
	}
	if instance.Services[0].TargetName != "gotenberg" || instance.Services[0].Action != "substitute" {
		t.Fatalf("athenapdf service = %#v", instance.Services[0])
	}
	if instance.Services[2].Action != "skip" {
		t.Fatalf("rsyslog service = %#v", instance.Services[2])
	}
}

func TestBuildPlanAppliesWodby2ServiceCompatibilityPolicy(t *testing.T) {
	enabled := true
	port := 80
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{{
				UUID: "instance-1", Name: "prod", Type: "prod", Status: "ok",
				Stack: Stack{Name: "drupal10"},
				Services: []Service{
					{Name: "apache", Enabled: true},
					{Name: "nginx", Enabled: true},
					{Name: "redis", Enabled: true},
					{Name: "varnish", Enabled: true},
				},
				Domains: []Domain{{
					UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
					Enabled: &enabled, Service: "apache", ServiceProtocol: "http", PortNumber: &port,
				}},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &plan.Apps[0].Instances[0]
	if service := preflightFindServicePlan(t, instance, "apache"); service.TargetName != "" || service.Action != "skip" {
		t.Fatalf("apache service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "redis"); service.TargetName != "valkey" || service.Action != "substitute" {
		t.Fatalf("redis service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "varnish"); service.TargetName != "vinyl" || service.Action != "substitute" {
		t.Fatalf("varnish service = %#v", service)
	}
	if route := instance.Routes[0]; route.Service != "nginx" || route.Action != "create_backend" {
		t.Fatalf("apache route replacement = %#v", route)
	}
	if !hasReviewMessage(plan.Review, SeveritySkipped, "Apache is intentionally not migrated") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "redis will be substituted with valkey") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "varnish will be substituted with vinyl") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "Apache-backed source route") {
		t.Fatalf("compatibility review = %#v", plan.Review)
	}

	explicit, err := BuildPlan(export, PlanOptions{
		SourceKind: "app",
		SourceID:   "app-1",
		TargetServiceMap: map[string]string{
			"instance-1/apache": "httpd",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	explicitInstance := &explicit.Apps[0].Instances[0]
	if service := preflightFindServicePlan(t, explicitInstance, "apache"); service.TargetName != "httpd" || service.Action != "map" {
		t.Fatalf("explicit apache service = %#v", service)
	}
	if route := explicitInstance.Routes[0]; route.Service != "apache" {
		t.Fatalf("explicit apache route = %#v", route)
	}
}

func TestBuildPlanSanitizesRepositoryCredentials(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App: &App{
			UUID: "app-1",
			Name: "demo",
			Repository: &Repository{
				UUID: "repo-1",
				URL:  "https://user:secret@git.example.com/org/repo.git?private_token=also-secret#fragment",
			},
		},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	repository := plan.Apps[0].Repository
	if repository == nil || repository.URL != "https://git.example.com/org/repo.git" || !repository.CredentialsRedacted {
		t.Fatalf("repository = %#v", repository)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private_token") {
		t.Fatalf("plan leaked repository credentials: %s", data)
	}

	scpURL, redacted := sanitizeRepositoryURL("git@git.example.com:org/repo.git?access_token=secret")
	if scpURL != "git@git.example.com:org/repo.git" || !redacted {
		t.Fatalf("scp URL = %q, redacted = %t", scpURL, redacted)
	}
	sshURL, redacted := sanitizeRepositoryURL("ssh://git@git.example.com/org/repo.git")
	if sshURL != "ssh://git@git.example.com/org/repo.git" || redacted {
		t.Fatalf("SSH URL = %q, redacted = %t", sshURL, redacted)
	}
	invalidURL, redacted := sanitizeRepositoryURL("https:/user:secret@git.example.com/org/repo.git")
	if invalidURL != "" || !redacted {
		t.Fatalf("invalid URL = %q, redacted = %t", invalidURL, redacted)
	}
	multiAtURL, redacted := sanitizeRepositoryURL("https://user:p@ss@git.example.com/org/repo.git")
	if multiAtURL != "https://git.example.com/org/repo.git" || !redacted {
		t.Fatalf("multi-@ URL = %q, redacted = %t", multiAtURL, redacted)
	}
}

func TestBuildPlanBlocksUnknownSourceEnvType(t *testing.T) {
	export := Export{
		Schema: ExportSchema,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "qa",
			Type:  "qa",
			Stack: Stack{Name: "drupal10"},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "blocked" || plan.Summary.Blocking != 1 {
		t.Fatalf("plan = %#v", plan)
	}

	plan, err = BuildPlan(export, PlanOptions{
		SourceKind:   "app",
		SourceID:     "app-1",
		TargetEnvMap: map[string]string{"qa": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "source_inventory_unvalidated" {
		t.Fatalf("status with explicit env map = %q", plan.Status)
	}
	instance := plan.Apps[0].Instances[0]
	if instance.TargetEnv != "test" || instance.TargetEnvType != "TEST" {
		t.Fatalf("target env = %#v", instance)
	}
}

func TestBuildPlanSupportsServerExportShape(t *testing.T) {
	export := Export{
		Schema: ExportSchema,
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo"},
			Instances: []Instance{{
				UUID:  "inst-1",
				Name:  "prod",
				Type:  "prod",
				Stack: Stack{Name: "drupal10"},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "server", SourceID: "server-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Apps != 1 || plan.Summary.Instances != 1 {
		t.Fatalf("summary = %#v", plan.Summary)
	}
	if plan.Apps[0].SourceUUID != "app-1" {
		t.Fatalf("app = %#v", plan.Apps[0])
	}
}

func TestBuildPlanUsesPerAppServerRepositoryTargets(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "server", UUID: "server-1"},
		Apps: []AppExport{
			{
				App: App{
					UUID: "app-1", Name: "first", Status: "ok",
					Repository: &Repository{UUID: "repo-1", Status: "ok"},
				},
				Instances: []Instance{{UUID: "inst-1", Name: "prod", Type: "prod", Status: "ok", Stack: Stack{Name: "drupal11"}}},
			},
			{
				App: App{
					UUID: "app-2", Name: "second", Status: "ok",
					Repository: &Repository{UUID: "repo-2", Status: "ok"},
				},
				Instances: []Instance{{UUID: "inst-2", Name: "prod", Type: "prod", Status: "ok", Stack: Stack{Name: "drupal11"}}},
			},
		},
	}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind: "server",
		SourceID:   "server-1",
		SkipData:   true,
		RepositoryByApp: map[string]RepositoryTargetPlan{
			"app-1":  {CIIntegrationID: 11, RemoteGitRepoID: "remote-1"},
			"second": {CIIntegrationID: 22, RemoteGitRepoID: "remote-2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Apps[0].Repository.CIIntegrationID != 11 || plan.Apps[0].Repository.RemoteGitRepoID != "remote-1" ||
		plan.Apps[1].Repository.CIIntegrationID != 22 || plan.Apps[1].Repository.RemoteGitRepoID != "remote-2" {
		t.Fatalf("repository plans = %#v", plan.Apps)
	}
	if _, err := BuildPlan(export, PlanOptions{
		SourceKind: "server",
		SourceID:   "server-1",
		RepositoryByApp: map[string]RepositoryTargetPlan{
			"missing": {CIIntegrationID: 33, RemoteGitRepoID: "remote-3"},
		},
	}); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("unknown repository mapping error = %v", err)
	}
}

func TestScopedMappingSupportsServerAppAndInstanceSelectors(t *testing.T) {
	mapping := map[string]string{
		"redis":                  "global",
		"prod/redis":             "instance",
		"first/redis":            "app",
		"app-1/prod/redis":       "app-instance-name",
		"app-1/instance-1/redis": "app-instance-uuid",
	}
	app := App{UUID: "app-1", Name: "first"}
	if got, found := scopedMapping(mapping, app, "instance-1", "prod", "redis"); !found || got != "app-instance-uuid" {
		t.Fatalf("most specific mapping = %q, found=%t", got, found)
	}
	delete(mapping, "app-1/instance-1/redis")
	if got, found := scopedMapping(mapping, app, "instance-1", "prod", "redis"); !found || got != "app-instance-name" {
		t.Fatalf("app/instance-name mapping = %q, found=%t", got, found)
	}
	if got, found := scopedMapping(mapping, App{UUID: "app-2", Name: "second"}, "instance-2", "stage", "redis"); !found || got != "global" {
		t.Fatalf("global fallback mapping = %q, found=%t", got, found)
	}
}

func TestBuildPlanSupportsEmptyV2ServerExport(t *testing.T) {
	plan, err := BuildPlan(
		Export{
			Schema: ExportSchemaV2,
			Source: &ExportSource{Kind: "server", UUID: "server-1"},
			Apps:   []AppExport{},
		},
		PlanOptions{SourceKind: "server", SourceID: "server-1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Apps == nil || len(plan.Apps) != 0 || plan.Summary.Apps != 0 ||
		plan.Status != "blocked" || plan.Summary.Blocking != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestBuildPlanSkipsTechnicalAndDisabledRoutes(t *testing.T) {
	enabled := true
	disabled := false
	sslRequired := true
	port := 80
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "prod",
			Type:  "prod",
			Stack: Stack{Name: "drupal10"},
			Domains: []Domain{
				{
					Name:          "technical.example",
					Type:          "technical",
					Enabled:       &enabled,
					SSLRequired:   &sslRequired,
					RedirectToWWW: true,
					Service:       "nginx",
					PortNumber:    &port,
				},
				{
					Name:          "disabled.example",
					Type:          "user",
					Enabled:       &disabled,
					SSLRequired:   &sslRequired,
					RedirectToWWW: true,
					Service:       "nginx",
					PortNumber:    &port,
				},
			},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}

	routes := plan.Apps[0].Instances[0].Routes
	if len(routes) != 2 {
		t.Fatalf("routes = %#v", routes)
	}
	if routes[0].Host != "disabled.example" || routes[0].Action != "skip_disabled" {
		t.Fatalf("disabled route = %#v", routes[0])
	}
	if routes[1].Host != "technical.example" || routes[1].Action != "skip_technical" {
		t.Fatalf("technical route = %#v", routes[1])
	}
	if routes[0].NeedsPortID || routes[1].NeedsPortID || !routes[0].Redirect || !routes[1].Redirect {
		t.Fatalf("skipped route metadata = %#v", routes)
	}
	if plan.Summary.Intentionally != 2 || plan.Summary.Blocking != 0 ||
		plan.Summary.Confirmation != 0 || plan.Summary.Manual != 0 {
		t.Fatalf("summary = %#v", plan.Summary)
	}
	if plan.Status != "source_inventory_unvalidated" {
		t.Fatalf("status = %q", plan.Status)
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), ".wodby.local") {
		t.Fatalf("plan invents a target domain: %s", data)
	}
}

func TestBuildPlanBlocksWildcardRouteBeforePrepare(t *testing.T) {
	enabled := true
	port := 80
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{{
				UUID: "instance-1", Name: "prod", Type: "prod", Status: "ok",
				Stack: Stack{Name: "drupal"},
				Domains: []Domain{{
					UUID: "domain-1", Name: "*.example.com", Type: "user",
					Status: "ok", Enabled: &enabled, Service: "nginx",
					ServiceProtocol: "http", PortNumber: &port,
				}},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	route := plan.Apps[0].Instances[0].Routes[0]
	if !route.ReviewRequired || route.Action != "unvalidated" || plan.Summary.Blocking == 0 {
		t.Fatalf("wildcard route was not blocked: route=%#v review=%#v", route, plan.Review)
	}
	if !hasReviewMessage(plan.Review, SeverityBlocking, "wildcard source routes") {
		t.Fatalf("wildcard blocker missing: %#v", plan.Review)
	}
}

func TestBuildPlanPreservesExplicitRedactionAndBasicAuthSemantics(t *testing.T) {
	notRedacted := false
	redacted := true
	port := 80
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "prod",
			Type:  "prod",
			Stack: Stack{Name: "drupal10"},
			BasicAuth: &BasicAuth{
				Enabled:          true,
				Login:            "ada",
				Password:         "available",
				Secret:           true,
				PasswordRedacted: &notRedacted,
			},
			Domains: []Domain{
				{Name: "open.example", Type: "user", Service: "nginx", PortNumber: &port},
				{Name: "protected.example", Type: "user", Protected: true, Service: "nginx", PortNumber: &port},
			},
			Services: []Service{{
				Name:    "php",
				Enabled: true,
				EnvVars: []EnvVar{
					{Name: "EMPTY_VALUE", Enabled: true, Protected: true, Origin: "custom", Redacted: &notRedacted},
					{Name: "REDACTED_VALUE", Enabled: true, Protected: true, Origin: "custom", Redacted: &redacted},
				},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 1 {
		t.Fatalf("blocking = %d, review = %#v", plan.Summary.Blocking, plan.Review)
	}

	instance := plan.Apps[0].Instances[0]
	if instance.BasicAuth.SecretRedacted {
		t.Fatalf("basic auth should honor explicit password_redacted=false: %#v", instance.BasicAuth)
	}
	if instance.Routes[0].Host != "open.example" || instance.Routes[0].BasicAuth {
		t.Fatalf("unprotected route basic auth = %#v", instance.Routes[0])
	}
	if instance.Routes[1].Host != "protected.example" || !instance.Routes[1].BasicAuth {
		t.Fatalf("protected route basic auth = %#v", instance.Routes[1])
	}
	if len(plan.Review) != 3 || plan.Review[0].Subject != "env var REDACTED_VALUE" ||
		plan.Review[1].Subject != "basic auth" ||
		plan.Review[2].Severity != SeverityConfirmation {
		t.Fatalf("review = %#v", plan.Review)
	}
}

func TestBuildPlanBlocksIncompleteBasicAuth(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:      "inst-1",
			Name:      "prod",
			Type:      "prod",
			Stack:     Stack{Name: "drupal10"},
			BasicAuth: &BasicAuth{Enabled: true},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 2 {
		t.Fatalf("blocking = %d, review = %#v", plan.Summary.Blocking, plan.Review)
	}
	if plan.Review[0].Subject != "basic auth" || plan.Review[1].Subject != "basic auth" {
		t.Fatalf("review = %#v", plan.Review)
	}
}

func TestBuildPlanPreservesSSLRequiredFalse(t *testing.T) {
	sslRequired := false
	port := 80
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "prod",
			Type:  "prod",
			Stack: Stack{Name: "drupal10"},
			Domains: []Domain{{
				Name:        "example.com",
				Type:        "user",
				SSLRequired: &sslRequired,
				Service:     "nginx",
				PortNumber:  &port,
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	settings := plan.Apps[0].Instances[0].Routes[0].Settings
	if len(settings) != 1 || settings[0] != (RouteSettingPlan{Name: "HTTPS_REDIRECT", Value: "false"}) {
		t.Fatalf("settings = %#v", settings)
	}
	if len(plan.Review) != 0 {
		t.Fatalf("review = %#v", plan.Review)
	}
	if plan.Apps[0].Instances[0].Routes[0].Action != "create_backend" {
		t.Fatalf("route = %#v", plan.Apps[0].Instances[0].Routes[0])
	}
}

func TestBuildPlanPreservesIndexedTrueAsNoIndexFalse(t *testing.T) {
	indexed := true
	port := 80
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "inst-1", Name: "prod", Type: "prod", Stack: Stack{Name: "drupal10"},
			Domains: []Domain{{
				Name: "example.com", Type: "user", Indexed: &indexed,
				Service: "nginx", PortNumber: &port,
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	settings := plan.Apps[0].Instances[0].Routes[0].Settings
	if len(settings) != 1 || settings[0] != (RouteSettingPlan{Name: "NO_INDEX", Value: "false"}) {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestBuildPlanUsesVerifiedTargetDiscovery(t *testing.T) {
	port := 80
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID:  "inst-1",
			Name:  "prod",
			Type:  "prod",
			Stack: Stack{Name: "drupal10"},
			Domains: []Domain{{
				Name:           "example.com",
				Type:           "user",
				Service:        "nginx",
				PortNumber:     &port,
				RedirectTarget: "www.example.com",
			}},
		}},
	}
	scope := TargetScopeDiscovery{
		User:       TargetCurrentUser{ID: 1},
		Membership: TargetOrgMembership{ID: 10, OrgID: 2, Role: "owner", Status: "ok"},
		Org:        TargetOrg{ID: 2, Name: "acme"},
		Project:    TargetProject{ID: 3, Name: "site", OrgID: 2},
		Cluster: TargetCluster{
			ID:     4,
			Name:   "prod",
			Status: "OK",
			OrgID:  2,
			Capabilities: TargetClusterCapabilities{
				EnvoyGateway:   true,
				RedirectRoutes: true,
			},
		},
	}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:    "app",
		SourceID:      "app-1",
		TargetOrg:     "acme",
		TargetProject: "site",
		TargetCluster: "prod",
		TargetScope:   &scope,
		TargetEnvs: map[string]TargetEnv{
			"prod": {ID: 5, Name: "prod", Type: "PROD", OrgID: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "target_scope_validated" {
		t.Fatalf("status = %q, review = %#v", plan.Status, plan.Review)
	}
	if plan.Target.OrgID != 2 || plan.Target.ProjectID != 3 || plan.Target.ClusterID != 4 ||
		!plan.Target.OrgOwnerOrAdminVerified || !plan.Target.DiscoveryVerified ||
		plan.Target.Capabilities == nil || !plan.Target.Capabilities.RedirectRoutes {
		t.Fatalf("target = %#v", plan.Target)
	}
	instance := plan.Apps[0].Instances[0]
	if instance.TargetEnvID != 5 || instance.TargetEnv != "prod" || instance.TargetEnvType != "PROD" {
		t.Fatalf("instance = %#v", instance)
	}
	if instance.Routes[0].Action != "create_redirect" {
		t.Fatalf("route = %#v", instance.Routes[0])
	}
}

func TestRedirectActionsRemainUnvalidatedWhenRouteChecksFail(t *testing.T) {
	port := 80
	negativePort := -1
	scope := &TargetScopeDiscovery{
		Cluster: TargetCluster{
			Capabilities: TargetClusterCapabilities{RedirectRoutes: true},
		},
	}
	cases := map[string]Domain{
		"missing service": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			PortNumber: &port, RedirectToWWW: true,
		},
		"missing port": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			Service: "nginx", RedirectToWWW: true,
		},
		"invalid port": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			Service: "nginx", PortNumber: &negativePort, RedirectToWWW: true,
		},
		"unstable status": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "updating",
			Service: "nginx", PortNumber: &port, RedirectToWWW: true,
		},
		"custom TLS": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			Service: "nginx", PortNumber: &port, RedirectToWWW: true, SSLCustom: true,
		},
		"unsupported protocol": {
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			Service: "nginx", PortNumber: &port, RedirectToWWW: true, ServiceProtocol: "tcp",
		},
	}

	for name, domain := range cases {
		t.Run(name, func(t *testing.T) {
			plan := Plan{}
			route := buildRoutePlan(
				&plan,
				App{Name: "demo"},
				Instance{Name: "prod"},
				domain,
				false,
				PlanOptions{TargetScope: scope},
				true,
			)
			if route.Action != "unvalidated" || !route.ReviewRequired {
				t.Fatalf("route = %#v, review = %#v", route, plan.Review)
			}
		})
	}
}

func TestBuildPlanMarksUnresolvedPayloadsForReview(t *testing.T) {
	port := 80
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{{
				UUID: "inst-1", Name: "prod", Type: "prod", Status: "ok",
				Stack: Stack{Name: "drupal10"},
				Services: []Service{{
					Name: "php", Enabled: true,
					Configuration: map[string]interface{}{"command": "php-fpm"},
					EnvVars:       []EnvVar{{Name: "CUSTOM", Value: "value", Enabled: true, Origin: "custom"}},
					CronJobs:      []CronJob{{Crontab: "@hourly", Command: "drush cron", Enabled: true}},
				}},
				Domains: []Domain{{
					UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
					Service: "nginx", PortNumber: &port,
				}},
				Backups: []Backup{{
					UUID: "backup-1", Component: "database", URL: "https://backups.example.com/database.sql", Status: "ok",
				}},
			}},
		}},
	}
	scope := TargetScopeDiscovery{
		User:       TargetCurrentUser{ID: 1},
		Membership: TargetOrgMembership{ID: 10, OrgID: 2, Role: "admin", Status: "ok"},
		Org:        TargetOrg{ID: 2, Name: "acme"},
		Project:    TargetProject{ID: 3, Name: "site", OrgID: 2},
		Cluster:    TargetCluster{ID: 4, Name: "prod", Status: "OK", OrgID: 2},
	}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:  "app",
		SourceID:    "app-1",
		TargetScope: &scope,
		TargetEnvs: map[string]TargetEnv{
			"prod": {ID: 5, Name: "prod", Type: "PROD", OrgID: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "blocked" || plan.Summary.Blocking != 1 ||
		plan.Summary.Confirmation < 3 || plan.Summary.Manual != 0 {
		t.Fatalf("status = %q, summary = %#v, review = %#v", plan.Status, plan.Summary, plan.Review)
	}
}

func TestBuildPlanCarriesSourceIssuesAndSkipsInfrastructureCron(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{{
				UUID:   "inst-1",
				Name:   "prod",
				Type:   "prod",
				Status: "ok",
				Stack:  Stack{Name: "drupal10"},
				Services: []Service{{
					Name:    "php",
					Enabled: true,
					CronJobs: []CronJob{
						{Crontab: "@daily", Command: "cleanup", Enabled: true, Classification: "source_only_infrastructure"},
						{Crontab: "@hourly", Command: "drush cron", Enabled: true, Classification: "application"},
					},
				}},
			}},
		}},
		Issues: []ExportIssue{{
			Code:     "cron.source_only_infrastructure",
			Severity: SeveritySkipped,
			Path:     "apps.app-1.instances.inst-1.properties.cron_tasks.lines.1",
			Message:  "source-only cron",
			Details: map[string]interface{}{
				"line_number": float64(1),
				"raw_line":    "@daily cleanup",
			},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.CronJobs != 1 || plan.Summary.Intentionally != 1 {
		t.Fatalf("summary = %#v, review = %#v", plan.Summary, plan.Review)
	}
	if plan.Review[0].Code != "cron.source_only_infrastructure" ||
		plan.Review[0].Path == "" ||
		plan.Review[0].Details != nil {
		t.Fatalf("review = %#v", plan.Review[0])
	}
}

func TestBuildPlanTreatsOptionalServicePropertiesAsEffectiveOnlyWhenServiceIsEnabled(t *testing.T) {
	base := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "instance-1", Name: "prod", Type: "prod",
			Stack:      Stack{Name: "drupal"},
			Properties: map[string]interface{}{"cache_redis": true},
			Services:   []Service{{Name: "php", Enabled: true}},
		}},
	}

	plan, err := BuildPlan(base, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("irrelevant optional-service default blocked migration: %#v", plan.Review)
	}

	disabledIntegration := cloneExportForTest(t, base)
	disabledIntegration.Instances[0].Properties["cache_redis"] = false
	disabledIntegration.Instances[0].Services = append(
		disabledIntegration.Instances[0].Services,
		Service{Name: "redis", Enabled: true},
	)
	plan, err = BuildPlan(disabledIntegration, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 1 ||
		!hasReviewMessage(plan.Review, SeverityBlocking, "application integration is disabled") {
		t.Fatalf("disabled effective integration was not blocked: %#v", plan.Review)
	}

	enabledIntegration := cloneExportForTest(t, disabledIntegration)
	enabledIntegration.Instances[0].Properties["cache_redis"] = true
	plan, err = BuildPlan(enabledIntegration, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "enabled source integration") {
		t.Fatalf("enabled effective integration was not reviewable: %#v", plan.Review)
	}
}

func TestBuildPlanMigratesPropertyDerivedPHPEnvironment(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "instance-1", Name: "prod", Type: "prod",
			Stack: Stack{Name: "drupal"},
			Properties: map[string]interface{}{
				"php_opcache": false,
				"php_xdebug":  true,
			},
			Services: []Service{{
				Name: "php", Enabled: true,
				EnvVars: []EnvVar{
					{Name: "PHP_OPCACHE_ENABLE", Value: "0", Enabled: true, Origin: "computed"},
					{Name: "PHP_XDEBUG", Value: "1", Enabled: true, Origin: "default"},
					{Name: "PHP_DEFAULT", Value: "unchanged", Enabled: true, Origin: "default"},
				},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	service := plan.Apps[0].Instances[0].Services[0]
	if service.EnvVars != 2 || plan.Summary.EnvVars != 2 {
		t.Fatalf("property-derived PHP environment was not selected: %#v", service)
	}
	if !hasReviewMessage(plan.Review, SeverityConfirmation, "OPcache") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "Xdebug") {
		t.Fatalf("property-derived behavior was not disclosed: %#v", plan.Review)
	}
}

func hasReviewMessage(items []ReviewItem, severity string, fragment string) bool {
	for _, item := range items {
		if item.Severity == severity && strings.Contains(item.Message, fragment) {
			return true
		}
	}
	return false
}

func TestBuildPlanOrderingIsDeterministic(t *testing.T) {
	port := 80
	appA := AppExport{
		App: App{UUID: "app-a", Name: "alpha"},
		Instances: []Instance{
			{
				UUID:  "instance-b",
				Name:  "stage",
				Type:  "stage",
				Stack: Stack{Name: "drupal10"},
			},
			{
				UUID:  "instance-a",
				Name:  "prod",
				Type:  "prod",
				Stack: Stack{Name: "drupal10"},
				Services: []Service{
					{Name: "rsyslog", Enabled: true},
					{Name: "athenapdf", Enabled: true},
				},
				Domains: []Domain{
					{UUID: "domain-b", Name: "b.example", Type: "user", Service: "nginx", PortNumber: &port},
					{UUID: "domain-a", Name: "a.example", Type: "user", Service: "nginx", PortNumber: &port},
				},
			},
		},
	}
	appB := AppExport{
		App: App{UUID: "app-b", Name: "beta"},
		Instances: []Instance{{
			UUID:  "instance-c",
			Name:  "dev",
			Type:  "dev",
			Stack: Stack{Name: "drupal10"},
		}},
	}
	reversedAppA := appA
	reversedAppA.Instances = []Instance{appA.Instances[1], appA.Instances[0]}
	reversedAppA.Instances[0].Services = []Service{appA.Instances[1].Services[1], appA.Instances[1].Services[0]}
	reversedAppA.Instances[0].Domains = []Domain{appA.Instances[1].Domains[1], appA.Instances[1].Domains[0]}

	exportA := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "server", UUID: "server-1"},
		Apps:   []AppExport{appB, appA},
		Issues: []ExportIssue{
			{Code: "same", Path: "path", Message: "message", Details: map[string]interface{}{"value": "b"}},
			{Code: "same", Path: "path", Message: "message", Details: map[string]interface{}{"value": "a"}},
		},
	}
	exportB := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "server", UUID: "server-1"},
		Apps:   []AppExport{reversedAppA, appB},
		Issues: []ExportIssue{
			{Code: "same", Path: "path", Message: "message", Details: map[string]interface{}{"value": "a"}},
			{Code: "same", Path: "path", Message: "message", Details: map[string]interface{}{"value": "b"}},
		},
	}
	planA, err := BuildPlan(exportA, PlanOptions{SourceKind: "server", SourceID: "server-1"})
	if err != nil {
		t.Fatal(err)
	}
	planB, err := BuildPlan(exportB, PlanOptions{SourceKind: "server", SourceID: "server-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(planA, planB) {
		t.Fatalf("plans differ by source ordering:\nA: %#v\nB: %#v", planA, planB)
	}
	if len(planA.Source.ExportDigest) == 0 || len(planA.PlanHash) != 64 {
		t.Fatalf("plan identity is incomplete: %#v", planA)
	}
}

func TestExportAndPlanDigestsIgnoreTransportMetadata(t *testing.T) {
	first := Export{
		Schema:         ExportSchemaV2,
		Source:         &ExportSource{Kind: "server", UUID: "server-1"},
		GeneratedAt:    100,
		ResponseDigest: strings.Repeat("a", 64),
		Apps:           []AppExport{},
	}
	second := first
	second.GeneratedAt = 200
	second.ResponseDigest = strings.Repeat("b", 64)

	firstDigest, err := first.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("content digests differ: %q != %q", firstDigest, secondDigest)
	}

	firstPlan, err := BuildPlan(first, PlanOptions{SourceKind: "server", SourceID: "server-1"})
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := BuildPlan(second, PlanOptions{SourceKind: "server", SourceID: "server-1"})
	if err != nil {
		t.Fatal(err)
	}
	if firstPlan.PlanHash != secondPlan.PlanHash {
		t.Fatalf("plan hashes differ: %q != %q", firstPlan.PlanHash, secondPlan.PlanHash)
	}
	if firstPlan.Source.GeneratedAt == secondPlan.Source.GeneratedAt {
		t.Fatalf("generation timestamps were not retained: %#v %#v", firstPlan.Source, secondPlan.Source)
	}
	if firstPlan.Source.ResponseDigest != "" || secondPlan.Source.ResponseDigest != "" {
		t.Fatalf("raw response digests must not be persisted: %#v %#v", firstPlan.Source, secondPlan.Source)
	}
}

func TestPlanHashTreatsOwnerAndAdminAsTheSameAuthorizationClass(t *testing.T) {
	owner := Plan{
		Schema: MigrationPlanSchema,
		Target: PlanTarget{
			OrgRole:                 "owner",
			OrgOwnerOrAdminVerified: true,
		},
	}
	ownerHash, err := owner.contentDigest()
	if err != nil {
		t.Fatal(err)
	}

	admin := owner
	admin.Target.OrgRole = "admin"
	adminHash, err := admin.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if adminHash != ownerHash {
		t.Fatalf("authorized role transition changed plan hash: owner=%q admin=%q", ownerHash, adminHash)
	}

	unverified := owner
	unverified.Target.OrgOwnerOrAdminVerified = false
	unverifiedHash, err := unverified.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if unverifiedHash == ownerHash {
		t.Fatal("loss of organization owner/admin authorization did not change plan hash")
	}

	tampered := owner
	tampered.Target.OrgRole = "member"
	tamperedHash, err := tampered.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if tamperedHash == ownerHash {
		t.Fatal("role outside the authorized owner/admin class did not change plan hash")
	}
}
