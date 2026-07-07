package wodby1

import "testing"

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
				Secret:  true,
			},
			Domains: []Domain{{
				Name:           "example.com",
				Primary:        true,
				Indexed:        &indexed,
				SSLRequired:    &sslRequired,
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
			Backups: []Backup{{UUID: "backup-1", URL: "https://example.com/backup.sql"}},
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
	if instance.TechnicalDomain != "demo.wodby.local" {
		t.Fatalf("technical domain = %q", instance.TechnicalDomain)
	}
	route := instance.Routes[0]
	if !route.NeedsPortID || route.PortNumber == nil || *route.PortNumber != 80 {
		t.Fatalf("route port resolution = %#v", route)
	}
	if len(route.Settings) != 2 || route.Settings[0] != "HTTPS_REDIRECT" || route.Settings[1] != "NO_INDEX" {
		t.Fatalf("route settings = %#v", route.Settings)
	}
	if instance.Services[1].TargetName != "gotenberg" || instance.Services[1].Action != "substitute" {
		t.Fatalf("athenapdf service = %#v", instance.Services[1])
	}
	if instance.Services[2].Action != "skip" {
		t.Fatalf("rsyslog service = %#v", instance.Services[2])
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
	if plan.Status != "clean" {
		t.Fatalf("status with explicit env map = %q", plan.Status)
	}
	if got := plan.Apps[0].Instances[0].TargetEnv; got != "test" {
		t.Fatalf("target env = %q", got)
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
