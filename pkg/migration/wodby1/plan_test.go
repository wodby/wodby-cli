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
			Backups: []Backup{{UUID: "backup-file-1", BackupUUID: "backup-1", Component: "database", URL: "https://example.com/backup.sql", Status: "ok"}},
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

func TestBuildPlanWarnsThatChangesAfterSelectedBackupAreExcluded(t *testing.T) {
	instance := Instance{
		UUID: "inst-1", Name: "prod", Type: "prod", Status: "ok", Updated: 200,
		Stack:      Stack{Name: "drupal11"},
		Properties: map[string]interface{}{"maintenance_mode": false},
		Backups: []Backup{{
			UUID: "file-db", BackupUUID: "backup-1", Component: "db",
			URL: "https://backups.example.test/db.sql.gz", Status: "ok",
			BackupCreated: 100, BackupUpdated: 110,
		}},
	}
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "instance", UUID: instance.UUID},
		Apps: []AppExport{{
			App:       App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{instance},
		}},
	}
	options := PlanOptions{
		SourceKind: "instance", SourceID: instance.UUID, RequireData: true,
	}

	plan, err := BuildPlan(export, options)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(plan.Review, SeverityConfirmation, "backup backup-1") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "changes made in Wodby 1 after this snapshot") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "optionally enable maintenance mode") {
		t.Fatalf("selected-backup warning = %#v", plan.Review)
	}
}

func TestSelectedBackupChangesExecutablePlanHash(t *testing.T) {
	base := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{
			Kind: "instance", ID: "instance-1", Schema: ExportSchemaV2,
			ConfigDigest: strings.Repeat("a", 64), BackupDigest: strings.Repeat("b", 64),
		},
		Target: PlanTarget{
			OrgID: 1, ClusterID: 2, DiscoveryVerified: true,
			OrgOwnerOrAdminVerified: true, OrgRole: "owner",
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1",
			Instances: []InstancePlan{{SourceUUID: "instance-1", Imports: []ImportPlan{{
				SourceUUID: "file-1", BackupUUID: "backup-1", BackupCreated: 100,
			}}}},
		}},
		Review: []ReviewItem{},
	}
	base.computeSummary()
	baseHash, err := base.contentDigest()
	if err != nil {
		t.Fatal(err)
	}

	changed := base
	changed.Source.BackupDigest = strings.Repeat("c", 64)
	changed.Apps[0].Instances[0].Imports[0].BackupUUID = "backup-2"
	changedHash, err := changed.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == baseHash {
		t.Fatal("selected backup change was incorrectly ignored")
	}
}

func TestContentDigestDoesNotMutateLiveSubscriptionUsage(t *testing.T) {
	plan := Plan{Target: PlanTarget{Subscription: &TargetOrgSubscription{
		Status: "ACTIVE",
		Plan: &TargetOrgSubscriptionPlan{
			Name: "developer", Usage: 8, UsageIncluded: 10,
		},
	}}}
	if _, err := plan.contentDigest(); err != nil {
		t.Fatal(err)
	}
	if got := plan.Target.Subscription.Plan.Usage; got != 8 {
		t.Fatalf("contentDigest mutated live subscription usage to %v", got)
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
					{Name: "athenapdf", Enabled: true},
					{Name: "crond", Enabled: true},
					{Name: "mailhog", Enabled: true},
					{Name: "memcache", Enabled: true},
					{Name: "nginx", Enabled: true},
					{Name: "pma", Enabled: true},
					{Name: "redis", Enabled: true},
					{Name: "rsyslog", Enabled: true},
					{Name: "sshd", Enabled: true},
					{Name: "varnish", Enabled: true},
					{Name: "xhprof", Enabled: true},
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
	if service := preflightFindServicePlan(t, instance, "athenapdf"); service.TargetName != "gotenberg" || service.Action != "substitute" {
		t.Fatalf("athenapdf service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "crond"); service.TargetName != "" || service.Action != "skip" {
		t.Fatalf("crond service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "mailhog"); service.TargetName != "mailpit" || service.Action != "substitute" {
		t.Fatalf("mailhog service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "memcache"); service.TargetName != "memcached" || service.Action != "substitute" {
		t.Fatalf("memcache service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "pma"); service.TargetName != "phpmyadmin" || service.Action != "substitute" {
		t.Fatalf("pma service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "redis"); service.TargetName != "valkey" || service.Action != "substitute" {
		t.Fatalf("redis service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "rsyslog"); service.TargetName != "" || service.Action != "skip" {
		t.Fatalf("rsyslog service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "sshd"); service.TargetName != "sshd" || service.Action != "substitute" {
		t.Fatalf("sshd service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "varnish"); service.TargetName != "vinyl" || service.Action != "substitute" {
		t.Fatalf("varnish service = %#v", service)
	}
	if service := preflightFindServicePlan(t, instance, "xhprof"); service.TargetName != "" || service.Action != "skip" {
		t.Fatalf("xhprof service = %#v", service)
	}
	if route := instance.Routes[0]; route.Service != "nginx" || route.Action != "create_backend" {
		t.Fatalf("apache route replacement = %#v", route)
	}
	if !hasReviewMessage(plan.Review, SeverityServiceWarning, "Apache is intentionally not migrated") ||
		!hasReviewMessage(plan.Review, SeverityConfirmation, "https://wodby.com/docs/2.0/stacks/catalog/gotenberg/#migrate-from-athenapdf") ||
		!hasReviewMessage(plan.Review, SeverityServiceWarning, "application cron jobs are migrated as Wodby 2 service cron schedules") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "mailhog will be substituted with mailpit") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "memcache will be substituted with memcached") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "managed phpmyadmin service") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "redis will be substituted with valkey") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "PHP SSH derivative service") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "varnish will be substituted with vinyl") ||
		!hasReviewMessage(plan.Review, SeverityServiceWarning, "xhprof is intentionally not migrated") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "Apache-backed source route") {
		t.Fatalf("compatibility review = %#v", plan.Review)
	}
	if plan.Summary.ServiceWarnings != 4 {
		t.Fatalf("enabled skipped service warnings = %d, review = %#v", plan.Summary.ServiceWarnings, plan.Review)
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

func TestBuildPlanRequiresExplicitServiceMappingsForFullyCustomStack(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "instance", UUID: "inst-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "custom-app", Status: "ok"},
			Instances: []Instance{{
				UUID: "inst-1", Name: "dev", Type: "dev", Status: "ok",
				Stack: Stack{Name: "customer-stack", Custom: true},
				Services: []Service{
					{
						Name: "runtime", Enabled: true,
						EnvVars: []EnvVar{{Name: "CUSTOM_DEFAULT", Value: "value", Enabled: true, Origin: "custom_stack"}},
					},
					{Name: "worker", Enabled: false},
				},
			}},
		}},
	}
	plan, err := BuildPlan(export, PlanOptions{SourceKind: "instance", SourceID: "inst-1", SkipCode: true, SkipData: true})
	if err != nil {
		t.Fatal(err)
	}
	instance := plan.Apps[0].Instances[0]
	runtime := preflightFindServicePlan(t, &instance, "runtime")
	if runtime.TargetName != "" || runtime.Action != "requires_mapping" || runtime.EnvVars != 1 {
		t.Fatalf("fully custom service plan = %#v", runtime)
	}
	worker := preflightFindServicePlan(t, &instance, "worker")
	if worker.TargetName != "" || worker.Action != "skip_disabled" {
		t.Fatalf("disabled fully custom service plan = %#v", worker)
	}
	if !hasReviewMessage(plan.Review, SeverityBlocking, "--target-service-map runtime=TARGET_SERVICE") {
		t.Fatalf("custom service mapping review = %#v", plan.Review)
	}
}

func TestBuildPlanTreatsForkedManagedDrupalAndWordPressStacksAsManaged(t *testing.T) {
	for _, test := range []struct {
		name     string
		stack    Stack
		blocking bool
	}{
		{name: "metadata drupal 10", stack: Stack{Name: "customer-fork", Type: "drupal10", Custom: true}},
		{name: "metadata drupal 8", stack: Stack{Name: "customer-fork", Type: "drupal8", Custom: true}, blocking: true},
		{name: "metadata drupal 9", stack: Stack{Name: "customer-fork", Type: "drupal9", Custom: true}, blocking: true},
		{name: "legacy ancestor wordpress", stack: Stack{Name: "customer-fork", Custom: true, AncestorName: "wordpress"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			export := Export{
				Schema: ExportSchemaV2,
				Source: &ExportSource{Kind: "instance", UUID: "inst-1"},
				Apps: []AppExport{{
					App: App{UUID: "app-1", Name: "forked-app", Status: "ok"},
					Instances: []Instance{{
						UUID: "inst-1", Name: "dev", Type: "dev", Status: "ok",
						Stack: test.stack,
						Services: []Service{
							{Name: "mailhog", Enabled: true},
							{Name: "redis", Enabled: true},
							{
								Name: "optional-extra", Enabled: true,
								EnvVars: []EnvVar{
									{Name: "SMTP_HOST", Value: "mailhog.source-instance", Enabled: true, Origin: "custom_stack"},
									{Name: "SMTP_PORT", Value: "25", Enabled: true, Origin: "custom_stack"},
								},
							},
						},
					}},
				}},
			}
			plan, err := BuildPlan(export, PlanOptions{SourceKind: "instance", SourceID: "inst-1", SkipCode: true, SkipData: true})
			if err != nil {
				t.Fatal(err)
			}
			instance := plan.Apps[0].Instances[0]
			redis := preflightFindServicePlan(t, &instance, "redis")
			if redis.TargetName != "valkey" || redis.Action != "substitute" {
				t.Fatalf("forked managed redis plan = %#v", redis)
			}
			extra := preflightFindServicePlan(t, &instance, "optional-extra")
			if extra.TargetName != "optional-extra" || extra.Action != "migrate" {
				t.Fatalf("forked managed optional service plan = %#v", extra)
			}
			if hasReviewMessage(plan.Review, SeverityBlocking, "requires an explicit target") {
				t.Fatalf("forked managed stack was treated as fully custom: %#v", plan.Review)
			}
			if !hasReviewMessage(plan.Review, SeverityMigration, "SMTP endpoint will be rewritten from mailhog:25 to mailpit:1025") {
				t.Fatalf("SMTP endpoint rewrite missing: %#v", plan.Review)
			}
			if got := hasReviewMessage(plan.Review, SeverityBlocking, "does not support Drupal"); got != test.blocking {
				t.Fatalf("Drupal compatibility blocker = %t, want %t: %#v", got, test.blocking, plan.Review)
			}
			if instance.Stack.Type != test.stack.Type {
				t.Fatalf("planned stack type = %q, want %q", instance.Stack.Type, test.stack.Type)
			}
		})
	}
}

func TestBuildPlanAllowsConfirmedDrupal8And9ButNeverDrupal7(t *testing.T) {
	for _, major := range []string{"8", "9"} {
		export := selectionTestExport("instance", "inst-dev")
		export.Apps[0].Instances = export.Apps[0].Instances[1:]
		export.Apps[0].Instances[0].Stack = Stack{Name: "customer-fork", Type: "drupal" + major, Custom: true}
		plan, err := BuildPlan(export, PlanOptions{
			SourceKind: "instance", SourceID: "inst-dev", SkipCode: true, SkipData: true,
			AllowUnsupportedDrupal: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if hasReviewMessage(plan.Review, SeverityBlocking, "does not support Drupal") ||
			!hasReviewMessage(plan.Review, SeverityConfirmation, "does not inspect application code") {
			t.Fatalf("Drupal %s review = %#v", major, plan.Review)
		}
	}

	export := selectionTestExport("instance", "inst-dev")
	export.Apps[0].Instances = export.Apps[0].Instances[1:]
	export.Apps[0].Instances[0].Stack = Stack{Name: "drupal7", Type: "drupal7"}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind: "instance", SourceID: "inst-dev", SkipCode: true, SkipData: true,
		AllowUnsupportedDrupal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(plan.Review, SeverityBlocking, "Drupal 7 cannot be migrated") {
		t.Fatalf("Drupal 7 review = %#v", plan.Review)
	}
}

func TestSourceStackMetadataTypeIsAuthoritativeOverAncestor(t *testing.T) {
	if got := sourceStackFamily(Stack{
		Name: "customer-stack", Type: "custom-application", Custom: true, AncestorName: "drupal11",
	}); got != "" {
		t.Fatalf("unknown metadata type classified as %q through its ancestor", got)
	}
	if got := sourceStackFamily(Stack{
		Name: "customer-stack", Type: "drupal9", Custom: true, AncestorName: "drupal11",
	}); got != "drupal9" {
		t.Fatalf("Drupal 9 metadata classified as %q", got)
	}
}

func TestBuildPlanShowsCompatibilityTargetsForDisabledServices(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "instance-1", Name: "dev", Type: "dev", Stack: Stack{Name: "drupal"},
			Services: []Service{
				{Name: "athenapdf"},
				{Name: "mailhog"},
				{Name: "pma"},
				{Name: "sshd"},
			},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	instance := &plan.Apps[0].Instances[0]
	for source, target := range map[string]string{
		"athenapdf": "gotenberg",
		"mailhog":   "mailpit",
		"pma":       "phpmyadmin",
		"sshd":      "sshd",
	} {
		service := preflightFindServicePlan(t, instance, source)
		if service.TargetName != target || service.Action != "skip_disabled" {
			t.Fatalf("%s service = %#v", source, service)
		}
	}
	if plan.Summary.Confirmation != 0 {
		t.Fatalf("disabled substitutions require confirmation: %#v", plan.Review)
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
	if repository == nil || repository.URL != "https://git.example.com/org/repo.git" ||
		repository.RepositoryName != "org/repo" || !repository.CredentialsRedacted {
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

func TestRepositoryNameFromURL(t *testing.T) {
	tests := map[string]string{
		"https://github.com/acme/example.git":             "acme/example",
		"ssh://git@gitlab.example.test/team/site.git":     "team/site",
		"git@bitbucket.org:acme/example.git":              "acme/example",
		"https://git.example.test/group%20name/repo.git/": "group name/repo",
		"https://git.example.test/":                       "",
		"":                                                "",
	}
	for input, want := range tests {
		if got := repositoryNameFromURL(input); got != want {
			t.Errorf("repositoryNameFromURL(%q) = %q, want %q", input, got, want)
		}
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
			"app-1":  {GitIntegrationID: 11, RepositoryName: "team/first"},
			"second": {GitIntegrationID: 22, RepositoryName: "team/second"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Apps[0].Repository.GitIntegrationID != 11 || plan.Apps[0].Repository.RepositoryName != "team/first" ||
		plan.Apps[1].Repository.GitIntegrationID != 22 || plan.Apps[1].Repository.RepositoryName != "team/second" {
		t.Fatalf("repository plans = %#v", plan.Apps)
	}
	if _, err := BuildPlan(export, PlanOptions{
		SourceKind: "server",
		SourceID:   "server-1",
		RepositoryByApp: map[string]RepositoryTargetPlan{
			"missing": {GitIntegrationID: 33, RepositoryName: "team/missing"},
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
		!hasReviewMessage(plan.Review, SeverityMigration, "Wodby 2 route auths will be created") {
		t.Fatalf("review = %#v", plan.Review)
	}
}

func TestBuildPlanBlocksUnmappableProtectedTechnicalRoute(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "inst-1", Name: "prod", Type: "prod", Stack: Stack{Name: "drupal10"},
			BasicAuth: &BasicAuth{Enabled: true, Login: "ada", Password: "secret"},
			Domains:   []Domain{{Name: "prod.demo.wodby.cloud", Type: "technical", Protected: true}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasReviewMessage(plan.Review, SeverityBlocking, "protected technical route is missing its service or port") {
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

func TestBuildPlanPreservesHSTS(t *testing.T) {
	port := 80
	tests := []struct {
		name        string
		hsts        bool
		subdomains  bool
		wantSetting *RouteSettingPlan
	}{
		{
			name:        "enabled",
			hsts:        true,
			wantSetting: &RouteSettingPlan{Name: TargetRouteSettingHSTS, Value: TargetRouteSettingHSTSEnabled},
		},
		{
			name:        "include subdomains",
			hsts:        true,
			subdomains:  true,
			wantSetting: &RouteSettingPlan{Name: TargetRouteSettingHSTS, Value: TargetRouteSettingHSTSIncludeSubdomains},
		},
		{
			name:       "subdomains without HSTS has no effect",
			subdomains: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			export := Export{
				Schema: ExportSchemaV1,
				App:    &App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{{
					UUID: "inst-1", Name: "prod", Type: "prod", Stack: Stack{Name: "drupal10"},
					Domains: []Domain{{
						Name: "example.com", Type: "user", HSTS: tt.hsts, HSTSSubdomains: tt.subdomains,
						Service: "nginx", PortNumber: &port,
					}},
				}},
			}

			plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
			if err != nil {
				t.Fatal(err)
			}
			settings := plan.Apps[0].Instances[0].Routes[0].Settings
			if tt.wantSetting == nil {
				if len(settings) != 0 {
					t.Fatalf("settings = %#v, want none", settings)
				}
			} else if len(settings) != 1 || settings[0] != *tt.wantSetting {
				t.Fatalf("settings = %#v, want %#v", settings, *tt.wantSetting)
			}
			if len(plan.Review) != 0 {
				t.Fatalf("review = %#v, want no HSTS blocker", plan.Review)
			}
		})
	}
}

func TestBuildRoutePlanPreservesHSTSOnRedirectRoute(t *testing.T) {
	indexed := false
	sslRequired := true
	port := 80
	plan := Plan{}
	route := buildRoutePlan(
		&plan,
		App{Name: "demo"},
		Instance{UUID: "inst-1", Name: "prod"},
		Domain{
			Name: "example.com", Type: "user", Service: "nginx", PortNumber: &port,
			Indexed: &indexed, SSLRequired: &sslRequired, HSTS: true, HSTSSubdomains: true,
			RedirectTarget: "www.example.com",
		},
		false,
		PlanOptions{TargetScope: &TargetScopeDiscovery{
			Cluster: TargetCluster{Capabilities: TargetClusterCapabilities{RedirectRoutes: true}},
		}},
		false,
	)

	if route.Action != "create_redirect" {
		t.Fatalf("action = %q, want create_redirect", route.Action)
	}
	want := []RouteSettingPlan{
		{Name: TargetRouteSettingHSTS, Value: TargetRouteSettingHSTSIncludeSubdomains},
		{Name: TargetRouteSettingHTTPSRedirect, Value: "true"},
		{Name: TargetRouteSettingNoIndex, Value: "true"},
	}
	if !reflect.DeepEqual(route.Settings, want) {
		t.Fatalf("settings = %#v, want %#v", route.Settings, want)
	}
	if len(plan.Review) != 0 {
		t.Fatalf("review = %#v, want no HSTS blocker", plan.Review)
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
		Org: TargetOrg{
			ID: 2, Name: "acme",
			Capabilities: &TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
		},
		Project: TargetProject{ID: 3, Name: "site", OrgID: 2},
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
		TargetStackID: 7,
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

func TestTargetWithoutCustomDomainsStagesRoutesDisabled(t *testing.T) {
	plan := Plan{Apps: []AppPlan{{Instances: []InstancePlan{{
		Routes: []RoutePlan{{Host: "example.com", Action: "create_backend"}},
	}}}}}
	scope := TargetScopeDiscovery{Org: TargetOrg{
		Capabilities: &TargetOrgCapabilities{CustomDomains: false, CronSchedules: true},
	}}

	validateTargetOrgFeatures(&plan, &scope)
	if len(plan.Review) != 1 {
		t.Fatalf("review = %#v", plan.Review)
	}
	item := plan.Review[0]
	if item.Severity != SeverityConfirmation || item.Subject != "custom domains" ||
		!strings.Contains(item.Message, "created disabled") {
		t.Fatalf("review item = %#v", item)
	}
}

func TestBuildPlanSupportsOrganizationOwnedTarget(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App: App{UUID: "app-1", Name: "demo", Status: "ok"},
			Instances: []Instance{{
				UUID: "inst-1", Name: "prod", Type: "prod", Status: "ok",
				Stack: Stack{Name: "drupal11"},
			}},
		}},
	}
	scope := TargetScopeDiscovery{
		User:       TargetCurrentUser{ID: 1},
		Membership: TargetOrgMembership{ID: 10, OrgID: 2, Role: "admin", Status: "ok"},
		Org: TargetOrg{
			ID: 2, Name: "acme",
			Capabilities: &TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
		},
		Cluster: TargetCluster{ID: 4, Name: "prod", Status: "OK", OrgID: 2},
	}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:    "app",
		SourceID:      "app-1",
		TargetOrg:     "acme",
		TargetCluster: "prod",
		TargetScope:   &scope,
		TargetStackID: 7,
		TargetEnvs: map[string]TargetEnv{
			"prod": {ID: 5, Name: "prod", Type: "PROD", OrgID: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "target_scope_validated" || plan.Target.ProjectID != 0 || plan.Target.Project != "" {
		t.Fatalf("target = %#v, status = %q, review = %#v", plan.Target, plan.Status, plan.Review)
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

func TestBuildRoutePlanKeepsCustomTLSRouteMigratable(t *testing.T) {
	port := 80
	plan := Plan{}
	route := buildRoutePlan(
		&plan,
		App{Name: "demo"},
		Instance{Name: "prod"},
		Domain{
			UUID: "domain-1", Name: "example.com", Type: "user", Status: "ok",
			Service: "nginx", PortNumber: &port, SSLCustom: true,
		},
		false,
		PlanOptions{TargetScope: &TargetScopeDiscovery{}},
		true,
	)
	if route.Action != "create_backend" || route.ReviewRequired || !route.SSL || !route.SSLCustom {
		t.Fatalf("route = %#v, review = %#v", route, plan.Review)
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
					UUID: "backup-file-1", BackupUUID: "backup-1", Component: "database", URL: "https://backups.example.com/database.sql", Status: "ok",
				}},
			}},
		}},
	}
	scope := TargetScopeDiscovery{
		User:       TargetCurrentUser{ID: 1},
		Membership: TargetOrgMembership{ID: 10, OrgID: 2, Role: "admin", Status: "ok"},
		Org: TargetOrg{
			ID: 2, Name: "acme",
			Capabilities: &TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
		},
		Project: TargetProject{ID: 3, Name: "site", OrgID: 2},
		Cluster: TargetCluster{ID: 4, Name: "prod", Status: "OK", OrgID: 2},
	}
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:    "app",
		SourceID:      "app-1",
		TargetScope:   &scope,
		TargetStackID: 7,
		TargetEnvs: map[string]TargetEnv{
			"prod": {ID: 5, Name: "prod", Type: "PROD", OrgID: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "target_scope_validated" || plan.Summary.Blocking != 0 ||
		plan.Summary.Migrations < 3 || plan.Summary.Confirmation != 0 || plan.Summary.Manual != 0 {
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
	disabledIntegration.Instances[0].Properties["cache_valkey"] = nil
	disabledIntegration.Instances[0].Services = append(
		disabledIntegration.Instances[0].Services,
		Service{Name: "redis", Enabled: true},
	)
	plan, err = BuildPlan(disabledIntegration, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 ||
		!hasReviewMessage(plan.Review, SeverityMigration, `mapped Wodby 2 service "valkey" will be disabled`) {
		t.Fatalf("disabled source integration did not disable the mapped service: %#v", plan.Review)
	}
	redisPlan := preflightFindServicePlan(t, &plan.Apps[0].Instances[0], "redis")
	if redisPlan.Enabled {
		t.Fatalf("redis target service should be disabled: %#v", redisPlan)
	}

	enabledIntegration := cloneExportForTest(t, disabledIntegration)
	enabledIntegration.Instances[0].Properties["cache_redis"] = true
	plan, err = BuildPlan(enabledIntegration, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 ||
		!hasReviewMessage(plan.Review, SeverityMigration, "enabled source cache integration") {
		t.Fatalf("enabled effective integration was not reviewable: %#v", plan.Review)
	}
}

func TestBuildPlanNamesResolvedTargetMailService(t *testing.T) {
	for _, test := range []struct {
		source string
		target string
	}{
		{source: "opensmtpd", target: "opensmtpd"},
		{source: "mailhog", target: "mailpit"},
	} {
		t.Run(test.source, func(t *testing.T) {
			export := Export{
				Schema: ExportSchemaV1,
				App:    &App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{{
					UUID:       "instance-1",
					Name:       "dev",
					Type:       "dev",
					Stack:      Stack{Name: "drupal"},
					Properties: map[string]interface{}{"mail_service": test.source},
					Services:   []Service{{Name: test.source, Enabled: true}},
				}},
			}

			plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
			if err != nil {
				t.Fatal(err)
			}
			expected := `source mail service "` + test.source + `" will map to Wodby 2 service "` + test.target + `"`
			if !hasReviewMessage(plan.Review, SeverityMigration, expected) {
				t.Fatalf("resolved mail service mapping %q missing: %#v", expected, plan.Review)
			}
		})
	}
}

func TestBuildPlanDescribesCreatedCronScheduleState(t *testing.T) {
	for _, test := range []struct {
		name         string
		cronAllowed  bool
		message      string
		confirmation bool
	}{
		{
			name:        "enabled",
			cronAllowed: true,
			message:     `1 Wodby 2 cron schedule will be added to target service "php" in enabled state`,
		},
		{
			name:         "disabled by subscription",
			cronAllowed:  false,
			message:      `1 Wodby 2 cron schedule will be added to target service "php" in disabled state because the target subscription does not allow cron execution`,
			confirmation: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			export := Export{
				Schema: ExportSchemaV1,
				App:    &App{UUID: "app-1", Name: "demo"},
				Instances: []Instance{{
					UUID: "instance-1", Name: "dev", Type: "dev", Stack: Stack{Name: "drupal"},
					Services: []Service{{
						Name: "php", Enabled: true,
						CronJobs: []CronJob{{Crontab: "0 * * * *", Command: "drush cron", Enabled: true}},
					}},
				}},
			}
			scope := TargetScopeDiscovery{Org: TargetOrg{
				ID: 1,
				Capabilities: &TargetOrgCapabilities{
					CustomDomains: true,
					CronSchedules: test.cronAllowed,
				},
			}}

			plan, err := BuildPlan(export, PlanOptions{
				SourceKind: "app", SourceID: "app-1", TargetScope: &scope,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !hasReviewMessage(plan.Review, SeverityMigration, test.message) {
				t.Fatalf("cron schedule migration detail %q missing: %#v", test.message, plan.Review)
			}
			schedules := plan.Apps[0].Instances[0].Services[0].CronSchedules
			if len(schedules) != 1 || schedules[0].Title != "Migrated Wodby 1 cron" ||
				schedules[0].Schedule != "0 * * * *" || schedules[0].Command != "drush cron" {
				t.Fatalf("cron schedule details = %#v", schedules)
			}
			wantState := "enabled"
			if !test.cronAllowed {
				wantState = "disabled by target subscription"
			}
			if schedules[0].TargetState != wantState {
				t.Fatalf("cron target state = %q, want %q", schedules[0].TargetState, wantState)
			}
			if got := hasReviewMessage(plan.Review, SeverityConfirmation, "migrated schedules will be created disabled"); got != test.confirmation {
				t.Fatalf("cron subscription confirmation = %t, want %t: %#v", got, test.confirmation, plan.Review)
			}
		})
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
	if !hasReviewMessage(plan.Review, SeverityMigration, "OPcache") ||
		!hasReviewMessage(plan.Review, SeverityMigration, "Xdebug") {
		t.Fatalf("property-derived behavior was not disclosed: %#v", plan.Review)
	}
}

func TestBuildPlanReportsCustomerWodbyNamespaceVariablesAsBlocking(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV1,
		App:    &App{UUID: "app-1", Name: "demo"},
		Instances: []Instance{{
			UUID: "instance-1", Name: "prod", Type: "prod",
			Stack: Stack{Name: "drupal"},
			Services: []Service{{
				Name: "php", Enabled: true,
				EnvVars: []EnvVar{
					{Name: "APP_MODE", Value: "production", Enabled: true, Origin: "custom"},
					{Name: "WODBY_CUSTOM", Value: "legacy", Enabled: true, Origin: "custom"},
					{Name: "WODBY_STACK_CUSTOM", Value: "legacy", Enabled: true, Origin: "custom_stack"},
					{Name: "WODBY_APP_NAME", Value: "generated", Enabled: true, Origin: "custom_stack"},
					{Name: "WODBY_MANAGED_DEFAULT", Value: "default", Enabled: true, Origin: "default"},
				},
			}},
		}},
	}

	plan, err := BuildPlan(export, PlanOptions{SourceKind: "app", SourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	service := plan.Apps[0].Instances[0].Services[0]
	if service.EnvVars != 1 || plan.Summary.EnvVars != 1 {
		t.Fatalf("only the non-reserved custom variable should be selected: %#v", service)
	}
	if plan.Summary.Blocking != 2 {
		t.Fatalf("blocking count = %d, want 2: %#v", plan.Summary.Blocking, plan.Review)
	}
	for _, name := range []string{"WODBY_CUSTOM", "WODBY_STACK_CUSTOM"} {
		if !hasReviewMessage(plan.Review, SeverityBlocking, name) {
			t.Fatalf("missing blocker for %s: %#v", name, plan.Review)
		}
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
