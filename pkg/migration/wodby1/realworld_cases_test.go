package wodby1

import (
	"context"
	"strings"
	"testing"
)

// The cases below follow configurations that are common in Wodby 1 production
// exports: Drupal on direct Git or third-party CI, custom domains on
// replacement services, database-only backups, per-component backup
// schedules, legacy mail settings exported as null, and cron schedules that
// use 7 for Sunday. Every fixture is synthetic.

// realDrupalCatalog models the managed Drupal 11 catalog stack: PHP holds the
// code, MariaDB the database, files-nfs the uploaded files, nginx and Vinyl
// serve HTTP, Mailpit catches mail and Valkey caches.
func realDrupalCatalog() preflightTargetCatalog {
	http := func(name string, number int, main bool) TargetServicePort {
		return TargetServicePort{Name: name, Number: number, Main: main, Protocol: "http"}
	}
	type service struct {
		id, revision  int
		name, revName string
		disabled      bool
		manifest      *TargetServiceManifest
	}
	services := []service{
		{11, 101, "php", "drupal11-php", false, &TargetServiceManifest{Name: "drupal11-php", Build: &TargetServiceBuildCapability{Connect: true},
			Options: []TargetServiceOption{{Version: "8.4", Default: true}, {Version: "8.3"}}, Settings: drupalPHPSettingManifest().Settings}},
		{12, 102, "nginx", "drupal11-nginx", false, &TargetServiceManifest{Name: "drupal11-nginx",
			Options: []TargetServiceOption{{Version: "1.29", Default: true}}, Endpoints: []TargetServiceEndpoint{{Name: "http", Ports: []TargetServicePort{http("http", 80, true)}}}}},
		{13, 103, "mariadb", "mariadb", false, &TargetServiceManifest{Name: "mariadb",
			Imports: []TargetServiceImportCapability{{Name: "database", Volume: "data"}}, Backups: []TargetServiceBackupCapability{{Name: "database"}},
			Options: []TargetServiceOption{{Version: "11.4", Default: true}}}},
		{14, 104, "valkey", "valkey", false, &TargetServiceManifest{Name: "valkey", Options: []TargetServiceOption{{Version: "8", Default: true}}}},
		{15, 105, "vinyl", "drupal-vinyl", false, &TargetServiceManifest{Name: "drupal-vinyl",
			Options: []TargetServiceOption{{Version: "7.6", Default: true}}, Endpoints: []TargetServiceEndpoint{{Name: "http", Ports: []TargetServicePort{http("http", 6081, true)}}}}},
		{16, 106, "mailpit", "mailpit", false, &TargetServiceManifest{Name: "mailpit", Endpoints: []TargetServiceEndpoint{{Name: "mailpit", Ports: []TargetServicePort{
			{Name: "smtp", Number: 1025, Protocol: "tcp"}, http("ui", 8025, false),
		}}}}},
		{17, 107, "opensmtpd", "opensmtpd", true, &TargetServiceManifest{Name: "opensmtpd"}},
		{19, 109, "files-nfs", "nfs-provisioner", false, &TargetServiceManifest{Name: "nfs-provisioner",
			Imports: []TargetServiceImportCapability{{Name: "files", Volume: "files"}}, Backups: []TargetServiceBackupCapability{{Name: "files"}}}},
	}
	catalog := preflightTargetCatalog{
		stacks:        map[string]TargetStack{"drupal11": {ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8}},
		stackServices: map[int][]TargetStackService{},
		revisions:     map[int]TargetServiceRevision{},
	}
	for _, s := range services {
		catalog.stackServices[71] = append(catalog.stackServices[71], TargetStackService{ID: s.id, Name: s.name, Disabled: s.disabled, ServiceRevID: s.revision})
		catalog.revisions[s.revision] = TargetServiceRevision{ID: s.revision, Name: s.revName, ServiceID: 200 + s.id, Manifest: s.manifest}
	}
	return catalog
}

// realDrupalExport is a Drupal 11 instance on direct Git deployment with a
// GitLab repository, OpenSMTPD mail and a complete database and files backup.
func realDrupalExport() Export {
	export := preflightFixtureExport(true)
	app := &export.Apps[0]
	app.App.Type = "drupal11"
	app.App.Repository.URL = "git@gitlab.com:acme/example.git"
	docroot, site := "web", "default"
	app.App.Docroot, app.App.SiteName = &docroot, &site
	instance := &app.Instances[0]
	instance.Stack = Stack{UUID: "stack-1", Name: "drupal11", Type: "drupal11", Version: "4.10.0"}
	instance.Services = []Service{
		{Name: "php", Version: "8.4", Enabled: true},
		{Name: "nginx", Version: "1.29", Enabled: true},
		{Name: "mariadb", Version: "11.4", Enabled: true},
		{Name: "redis", Version: "7", Enabled: false},
		{Name: "varnish", Version: "6.0", Enabled: false},
		{Name: "mailhog", Enabled: false},
		{Name: "opensmtpd", Enabled: true},
	}
	instance.Properties["mail_service"] = "opensmtpd"
	instance.Properties["cache_redis"] = false
	instance.Domains = []Domain{{UUID: "d-tech", Name: "nginx-prod-example.wod.by", Type: "technical", Status: "ok", Service: "nginx", PortNumber: intPointer(80)}}
	instance.Backups = []Backup{
		{UUID: "f-db", BackupUUID: "b-1", Component: "db", Status: "ok", URL: "https://node.example.test/db.sql.gz", Size: 10, Created: 1, BackupCreated: 1},
		{UUID: "f-files", BackupUUID: "b-1", Component: "files", Status: "ok", URL: "https://node.example.test/files.tar.gz", Size: 10, Created: 1, BackupCreated: 1},
	}
	instance.BackupConfig = &BackupConfig{Enabled: true, Depth: 7, Time: "03:00"}
	return export
}

func intPointer(value int) *int    { return &value }
func boolPointer(value bool) *bool { return &value }

type realCaseResult struct {
	plan     Plan
	prepared PreparedMigration
}

func runRealCase(t *testing.T, mutate func(*Export), configure func(*PlanOptions), catalogMutate func(*preflightTargetCatalog)) realCaseResult {
	t.Helper()
	export := realDrupalExport()
	if mutate != nil {
		mutate(&export)
	}
	catalog := realDrupalCatalog()
	if catalogMutate != nil {
		catalogMutate(&catalog)
	}
	api := newPreflightTargetAPI(t, catalog)
	options := preflightOwnerPlanOptions()
	options.TargetScope.Org.Capabilities.AutoBackups = true
	options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
	options.RequireData = true
	if configure != nil {
		configure(&options)
	}
	plan := preflightBuildPlan(t, export, options)
	prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	return realCaseResult{plan: plan, prepared: prepared}
}

func (r realCaseResult) review(severity, subject string) []string {
	messages := []string{}
	for _, item := range r.plan.Review {
		if item.Severity == severity && (subject == "" || item.Subject == subject) {
			messages = append(messages, item.Message)
		}
	}
	return messages
}

func (r realCaseResult) route(host string) RoutePlan {
	for _, route := range r.plan.Apps[0].Instances[0].Routes {
		if route.Host == host {
			return route
		}
	}
	return RoutePlan{}
}

func addUserDomain(e *Export, domain Domain) {
	domain.Type, domain.Status, domain.Enabled = "user", "ok", boolPointer(true)
	e.Apps[0].Instances[0].Domains = append(e.Apps[0].Instances[0].Domains, domain)
}

func enableSourceService(e *Export, name string) {
	for index := range e.Apps[0].Instances[0].Services {
		if e.Apps[0].Instances[0].Services[index].Name == name {
			e.Apps[0].Instances[0].Services[index].Enabled = true
		}
	}
}

func TestRealCaseBaselineMigratesCodeDataAndRoutes(t *testing.T) {
	result := runRealCase(t, nil, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	instance := result.prepared.Instances[0]
	if instance.BuildSource == nil || *instance.BuildSource.Input.GitRef != "main" || !instance.UsesWodbyCI {
		t.Fatalf("Git deployment must build the deployed branch with Wodby CI: %#v", instance.BuildSource)
	}
	if _, ok := instance.ImportByComponent["db"]; !ok {
		t.Fatal("the database must be imported")
	}
	if _, ok := instance.ImportByComponent["files"]; !ok {
		t.Fatal("the files must be imported")
	}
	if route := result.route("nginx-prod-example.wod.by"); route.Action != "skip_technical" {
		t.Fatalf("technical route action = %q", route.Action)
	}
}

// Wodby 1 serves the MailHog UI on port 80; Mailpit serves it on 8025. A custom
// domain on it must use 8025 instead of failing at apply.
func TestRealCaseRouteMovesToTheReplacementServicePort(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		enableSourceService(e, "mailhog")
		e.Apps[0].Instances[0].Properties["mail_service"] = "mailhog"
		addUserDomain(e, Domain{UUID: "d-mail", Name: "mail.example.com", Service: "mailhog", PortNumber: intPointer(80)})
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	route := result.route("mail.example.com")
	if route.Action != "create_backend" || route.PortNumber == nil || *route.PortNumber != 8025 {
		t.Fatalf("route = %#v", route)
	}
	if notes := result.review(SeverityMigration, "route mail.example.com"); len(notes) != 1 || !strings.Contains(notes[0], "port 8025") {
		t.Fatalf("the port change must be shown: %q", notes)
	}
}

func TestRealCaseRouteBlocksWithoutASinglePublicPort(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		enableSourceService(e, "mailhog")
		e.Apps[0].Instances[0].Properties["mail_service"] = "mailhog"
		addUserDomain(e, Domain{UUID: "d-mail", Name: "mail.example.com", Service: "mailhog", PortNumber: intPointer(80)})
	}, nil, func(catalog *preflightTargetCatalog) {
		revision := catalog.revisions[106]
		revision.Manifest = &TargetServiceManifest{Name: "mailpit", Endpoints: []TargetServiceEndpoint{{Name: "mailpit", Ports: []TargetServicePort{
			{Name: "ui", Number: 8025, Protocol: "http"}, {Name: "api", Number: 8026, Protocol: "http"},
		}}}}
		catalog.revisions[106] = revision
	})
	if blockers := result.review(SeverityBlocking, "route mail.example.com"); len(blockers) != 1 {
		t.Fatalf("an unresolvable port must block the preview: %q", result.review(SeverityBlocking, ""))
	}
}

func TestRealCaseRouteOnASharedPortIsKept(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		enableSourceService(e, "varnish")
		addUserDomain(e, Domain{UUID: "d-www", Name: "example.com", Service: "varnish", PortNumber: intPointer(6081), Primary: true, SSL: true})
	}, nil, nil)
	if route := result.route("example.com"); route.PortNumber == nil || *route.PortNumber != 6081 || route.Action != "create_backend" {
		t.Fatalf("route = %#v", route)
	}
	if notes := result.review(SeverityMigration, "route example.com"); len(notes) != 0 {
		t.Fatalf("an unchanged port needs no note: %q", notes)
	}
}

// Wodby 1 can turn off automatic files backups, so the newest backups often
// hold the database only.
func TestRealCaseDatabaseOnlyBackupWarnsAboutFiles(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		e.Apps[0].Instances[0].Backups = e.Apps[0].Instances[0].Backups[:1]
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	if warnings := result.review(SeverityConfirmation, "files backup"); len(warnings) != 1 {
		t.Fatalf("a database-only backup must say that files are not migrated: %q", result.plan.Review)
	}
	if baseline := runRealCase(t, nil, nil, nil); len(baseline.review(SeverityConfirmation, "files backup")) != 0 {
		t.Fatal("a complete backup needs no files warning")
	}
}

func TestRealCaseWithoutBackupsBlocksUnlessDataIsSkipped(t *testing.T) {
	noBackups := func(e *Export) { e.Apps[0].Instances[0].Backups = nil }
	if blockers := runRealCase(t, noBackups, nil, nil).review(SeverityBlocking, "source backup"); len(blockers) != 1 {
		t.Fatal("an instance without a successful backup must block")
	}
	skipped := runRealCase(t, noBackups, func(o *PlanOptions) { o.RequireData, o.SkipData = false, true }, nil)
	if blockers := skipped.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("--skip-data must not block: %q", blockers)
	}
}

func TestRealCaseBackupSchedules(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		e.Apps[0].Instances[0].BackupConfig.Schedules = map[string]BackupComponentSchedule{
			"db":    {Enabled: true, Crontab: "0 1,5,9,13,17,21 * * *"},
			"files": {Enabled: false, Crontab: "0 3 * * *"},
		}
	}, nil, nil)
	destination := result.prepared.Instances[0].BackupDestination
	if destination == nil || destination.Disabled || !destination.presetDisabled("files") || destination.presetDisabled("database") {
		t.Fatalf("only the files preset must be disabled: %#v", destination)
	}
	if warnings := result.review(SeverityServiceWarning, "automatic backups"); len(warnings) != 1 || !strings.Contains(warnings[0], "0 1,5,9,13,17,21 * * *") {
		t.Fatalf("a schedule Wodby 2 cannot repeat must be reported: %q", warnings)
	}
	if files, database := migrationBackupPresetInput(31, "files", *destination), migrationBackupPresetInput(32, "database", *destination); !files.Disabled || database.Disabled {
		t.Fatalf("preset inputs: files disabled=%v, database disabled=%v", files.Disabled, database.Disabled)
	}
}

func TestRealCaseLegacyMailSettings(t *testing.T) {
	// Wodby 1 exports null for a mail service that was never selected.
	result := runRealCase(t, func(e *Export) {
		e.Apps[0].Instances[0].Properties["mail_service"] = nil
		e.Apps[0].Instances[0].Properties["php_mail_catcher"] = false
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("a null mail_service must fall back to php_mail_catcher: %q", blockers)
	}
	// An empty mail_service also leaves the decision to php_mail_catcher.
	result = runRealCase(t, func(e *Export) {
		e.Apps[0].Instances[0].Properties["mail_service"] = ""
		e.Apps[0].Instances[0].Properties["php_mail_catcher"] = true
		enableSourceService(e, "mailhog")
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	links := result.prepared.StackConfiguration.Services["php"].Links
	if len(links) != 1 || links[0].LinkedServiceName != "mailpit" {
		t.Fatalf("php_mail_catcher=true must link Mailpit: %#v", links)
	}
}

func TestRealCaseNullCacheSettingIsIgnored(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		enableSourceService(e, "redis")
		e.Apps[0].Instances[0].Properties["cache_redis"] = nil
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("a null cache setting must not block: %q", blockers)
	}
}

func TestRealCaseCronSundayAsSeven(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		e.Apps[0].Instances[0].Services[0].CronJobs = []CronJob{{Title: "Migrated cron 1", Crontab: "0 3 * * 7", Command: "drush cron", Enabled: true, Classification: "application"}}
	}, nil, nil)
	schedules := result.prepared.StackConfiguration.Services["php"].CronSchedules
	found := false
	for _, schedule := range schedules {
		if schedule.Command == "drush cron" {
			found = schedule.Crontab == "0 3 * * 0"
		}
	}
	if !found {
		t.Fatalf("Sunday must be written as 0: %#v", schedules)
	}
}

func TestMigratedCrontab(t *testing.T) {
	for input, want := range map[string]string{
		"0 3 * * 7":     "0 3 * * 0",
		"0 3 * * 1,7":   "0 3 * * 1,0",
		"0 3 * * 5-7":   "0 3 * * 5-6,0",
		"0 3 * * 0,7":   "0 3 * * 0",
		"*/5  * * * *":  "*/5  * * * *",
		"0 3 * * 1-5":   "0 3 * * 1-5",
		"@daily":        "@daily",
		"0 3 17 * *":    "0 3 17 * *",
		"0 3 * * 1-7/2": "0 3 * * 1-7/2",
	} {
		if got := migratedCrontab(input); got != want {
			t.Errorf("migratedCrontab(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRealCaseRepositoryURLForms(t *testing.T) {
	for url, want := range map[string]string{
		"git@gitlab.com:acme/example.git":                      "acme/example",
		"git@github.com:acme/example.git":                      "acme/example",
		"ssh://git@bitbucket.org/acme/example.git":             "acme/example",
		"ssh://git@git.example.org:2222/group/sub/example.git": "group/sub/example",
		"https://gitlab.com/group/sub/example.git":             "group/sub/example",
	} {
		if got := repositoryNameFromURL(url); got != want {
			t.Errorf("repositoryNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestRealCaseSourceStatesThatBlock(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate  func(*Export)
		subject string
	}{
		"unreachable instance": {func(e *Export) { e.Apps[0].Instances[0].Status = "unreachable" }, "instance status"},
		"Drupal 9 stack": {func(e *Export) {
			e.Apps[0].App.Type = "drupal9"
			e.Apps[0].Instances[0].Stack = Stack{UUID: "stack-9", Name: "drupal9", Type: "drupal9", Version: "4.5.0"}
		}, "source stack compatibility"},
	} {
		t.Run(name, func(t *testing.T) {
			if blockers := runRealCase(t, tc.mutate, nil, nil).review(SeverityBlocking, tc.subject); len(blockers) != 1 {
				t.Fatalf("expected one %q blocker", tc.subject)
			}
		})
	}
}

func TestRealCaseThirdPartyCIWithoutRepository(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		e.Apps[0].App.Repository = nil
		e.Apps[0].Instances[0].Properties["deployment_type"] = "ci"
		e.Apps[0].Instances[0].Properties["ci_provider"] = "gitlab"
	}, func(o *PlanOptions) { o.Repository = RepositoryTargetPlan{} }, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	instance := result.prepared.Instances[0]
	if !instance.ExternalCIOnly || instance.UsesWodbyCI || len(result.review(SeverityManual, "Custom CI bootstrap build")) != 1 {
		t.Fatalf("CI without a repository must use Custom CI: %#v", instance)
	}
}

// A protected technical route of a replacement service gets basic auth on the
// port the target serves.
func TestRealCaseProtectedTechnicalRouteUsesTheReplacementPort(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		enableSourceService(e, "mailhog")
		instance := &e.Apps[0].Instances[0]
		instance.Properties["mail_service"] = "mailhog"
		instance.BasicAuth = &BasicAuth{Enabled: true, Login: "user", Password: "secret", Secret: true}
		instance.Domains = append(instance.Domains, Domain{UUID: "d-mh", Name: "mailhog-prod-example.wod.by", Type: "technical", Status: "ok", Service: "mailhog", PortNumber: intPointer(80), Protected: true})
	}, nil, nil)
	route := result.route("mailhog-prod-example.wod.by")
	if route.Action != "skip_technical" || !route.BasicAuth || route.PortNumber == nil || *route.PortNumber != 8025 {
		t.Fatalf("route = %#v", route)
	}
}

// A selected mail service that is disabled sends no mail in Wodby 1; the
// migration warns and creates no mail link instead of blocking.
func TestRealCaseDisabledMailServiceSendsNoMail(t *testing.T) {
	result := runRealCase(t, func(e *Export) {
		for index := range e.Apps[0].Instances[0].Services {
			if e.Apps[0].Instances[0].Services[index].Name == "opensmtpd" {
				e.Apps[0].Instances[0].Services[index].Enabled = false
			}
		}
	}, nil, nil)
	if blockers := result.review(SeverityBlocking, ""); len(blockers) != 0 {
		t.Fatalf("a disabled mail service must not block: %q", blockers)
	}
	if warnings := result.review(SeverityConfirmation, "mail service"); len(warnings) != 1 {
		t.Fatalf("mail service warnings = %q", warnings)
	}
	if links := result.prepared.StackConfiguration.Services["php"].Links; len(links) != 0 {
		t.Fatalf("no mail link expected: %#v", links)
	}
}
