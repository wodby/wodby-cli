package wodby1

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPrintReviewUsesTablesAndSeparateReviewSections(t *testing.T) {
	port := 80
	plan := Plan{
		Status:   "blocked",
		PlanHash: "plan-hash",
		Source: PlanSource{
			Kind:         "instance",
			ID:           "instance-1",
			ExportDigest: "export-digest",
		},
		Target: PlanTarget{
			Org:                     "chingis23",
			OrgID:                   48350498223106,
			OrgName:                 "chingis23",
			OrgRole:                 "owner",
			Cluster:                 "213518072237423",
			ClusterID:               213518072237423,
			ClusterName:             "sdfbsdgre",
			ClusterStatus:           "ok",
			OrgOwnerOrAdminVerified: true,
			DiscoveryVerified:       true,
			Capabilities: &TargetClusterCapabilities{
				EnvoyGateway:   true,
				RedirectRoutes: true,
			},
			OrgCapabilities: &TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
		},
		Summary: PlanSummary{
			Apps: 1, Instances: 1, Services: 2, Routes: 1, Imports: 1,
			Migrations: 1, ServiceWarnings: 1, Blocking: 1, Confirmation: 1, Manual: 1, Intentionally: 1,
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1",
			Name:       "demo",
			Title:      "Demo",
			Repository: &RepositoryPlan{
				Action: "connect", GitIntegrationID: 44, RepositoryName: "acme/demo",
				RemoteGitRepoID: "remote-repo-17", TargetService: "php",
			},
			Instances: []InstancePlan{{
				Name:       "dev",
				Title:      "Dev",
				SourceType: "dev",
				TargetEnv:  "dev",
				Stack: StackPlan{
					Name: "drupal", Target: "drupal11", CatalogName: "drupal11",
					CreateTarget: true, TargetVersion: "revision-4",
				},
				Services: []ServicePlan{
					{SourceName: "mailhog", TargetName: "mailpit", Enabled: true, Action: "substitute", Settings: 1},
					{SourceName: "php", TargetName: "php", Enabled: true, Action: "migrate", CronJobs: 1,
						CronSchedules: []CronSchedulePlan{{Title: "Drupal cron", Schedule: "0 * * * *", Command: "drush cron", TargetState: "disabled by target subscription"}}},
					{SourceName: "xhprof", Enabled: true, Action: "skip"},
				},
				Routes: []RoutePlan{{
					Host: "example.test", Action: "create_backend", Type: "user",
					Service: "nginx", PortNumber: &port, Primary: true,
				}},
				Imports: []ImportPlan{{
					Component: "db", Action: "import", TargetService: "mariadb", TargetImport: "database",
					BackupUUID: "backup-2023-11-14", BackupCreated: 1700000000, Size: 1048576,
				}},
			}},
		}},
		Review: []ReviewItem{
			{Severity: SeverityBlocking, App: "demo", Instance: "dev", Subject: "backup", Message: "create a backup"},
			{Severity: SeverityMigration, App: "demo", Instance: "dev", Subject: "service mailhog", Message: "mailhog will map to mailpit"},
			{Severity: SeverityConfirmation, App: "demo", Instance: "dev", Subject: "PHP version", Message: "source PHP 7.3 is EOL; target PHP 8.3 requires review"},
			{Severity: SeverityManual, App: "demo", Instance: "dev", Subject: "DNS", Message: "switch after verification"},
			{Severity: SeverityServiceWarning, App: "demo", Instance: "dev", Subject: "service xhprof", Message: "enabled but not migrated"},
			{Severity: SeveritySkipped, App: "demo", Instance: "dev", Subject: "route disabled.example", Message: "disabled source route is not migrated"},
		},
	}

	gitRef := "main"
	gitRefType := TargetGitRefBranch
	devEnvType := "DEV"
	phpReplicas := 2
	phpRequestCPU := 250
	mailLimitMem := 512
	prepared := PreparedMigration{
		App: AppExport{App: App{UUID: "app-1"}},
		Instances: []PreparedInstance{{
			Source: Instance{Name: "dev", Title: "Dev", Services: []Service{{Name: "mailhog"}}},
			Services: map[string]PreparedService{
				"mailhog": {
					Target:    TargetStackServiceInspection{StackService: TargetStackService{Name: "mailpit"}},
					Resources: &PreparedServiceResources{Workload: "mailpit", Container: "mailpit", LimitMem: &mailLimitMem},
				},
			},
			BuildSource: &PreparedBuildSource{Input: TargetBuildSourceInput{
				GitRef: &gitRef, GitRefType: &gitRefType,
			}},
		}},
		StackConfiguration: PreparedStackConfiguration{Services: map[string]PreparedStackServiceConfiguration{
			"php": {
				Replicas:  &phpReplicas,
				Resources: &PreparedServiceResources{Workload: "php", Container: "php", RequestCPU: &phpRequestCPU},
				EnvVars:   []PreparedStackEnvVar{{Name: "APP_MODE", Value: "development", EnvType: &devEnvType}},
				SettingMappings: []PreparedStackSettingMapping{
					{Source: "Wodby 1 app docroot", Name: "docroot", Value: "web", Action: "already matches target"},
					{Source: "Wodby 1 app site directory", Name: "sitedir", Value: "test", Action: "set stack override"},
				},
			},
		}},
	}

	var output bytes.Buffer
	PrintReview(&output, plan, prepared)
	text := output.String()
	for _, expected := range []string{
		"Field",
		"Value",
		"Item                   Count",
		"App 1/1: Demo → demo",
		"Migrations:",
		"Target stack (shared by all instances):",
		"Repository and CI:",
		"connect  Wodby CI (default)  ID 44            acme/demo          exact match found  branch \"main\"  php",
		"Instance 1/1: Dev → dev (dev → dev)",
		"create and configure  drupal         new from catalog drupal11  revision-4",
		"setting php.docroot",
		"Wodby 1 app docroot → \"web\"; already matches target",
		"setting php.sitedir",
		"Wodby 1 app site directory → \"test\"; set stack override",
		"Stack service environment variables:",
		"php      APP_MODE  \"development\"  DEV instances",
		"Stack service capacity (shared by all instances):",
		"php      2         CPU request 250m  php/php",
		"App-service capacity overrides:",
		"mailhog         mailpit         -         memory limit 512Mi  mailpit/mailpit",
		"Source   Target   Source version  Target version  Version action",
		"mailhog  mailpit  -               -               -               enabled  substitute",
		"3 source service(s) across 1 instance(s), consolidated into 3 mapping pattern(s)",
		"Service mapping overview:",
		"Cron jobs → cron schedules:",
		"php             php             Drupal cron  0 * * * *  drush cron  disabled by target subscription",
		"Custom domains:",
		"Domain        Action  Target    Target state             Options",
		"example.test  serve   nginx:80  will be created enabled  primary",
		"db         import  mariadb:database  backup-2023-11-14  14 Nov 2023, 22:13 UTC  1.0 MiB",
		"Warnings (1):",
		"Enabled services not migrated (1):",
		"Blocking (1):",
		"Manual follow-up (1):",
		"Intentionally skipped (1):",
		"Subject      Details",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("review output does not contain %q:\n%s", expected, text)
		}
	}
	for _, duplicate := range []string{"Converted stack settings", "Drupal app settings"} {
		if strings.Contains(text, duplicate) {
			t.Fatalf("review output contains duplicate settings section %q:\n%s", duplicate, text)
		}
	}
	for field, value := range map[string]string{
		"Source":              "Demo - Dev (dev)",
		"Target organization": "chingis23",
		"Target cluster":      "sdfbsdgre",
		"Target CI":           "Wodby CI (built-in)",
	} {
		if !reviewTableContainsRow(text, field, value) {
			t.Fatalf("review overview table does not contain %q = %q:\n%s", field, value, text)
		}
	}
	for _, internalDetail := range []string{
		"Status",
		"Source export digest",
		"Plan hash",
		"Intended target",
		"Target organization owner/admin",
		"Target discovery",
		"Resolved target",
		"Target capabilities",
		"app-1",
		"instance-1",
		"48350498223106",
		"213518072237423",
	} {
		if strings.Contains(text, internalDetail) {
			t.Fatalf("review output exposes internal detail %q:\n%s", internalDetail, text)
		}
	}
	if strings.Contains(text, "Review items:") || strings.Contains(text, "[blocking]") || strings.Contains(text, "Additional migration details") {
		t.Fatalf("review output still uses the combined review list:\n%s", text)
	}

	appIndex := strings.Index(text, "App 1/1: Demo → demo")
	instanceIndex := strings.Index(text, "Instance 1/1: Dev → dev")
	warningsIndex := strings.Index(text, "Warnings (1):")
	blockingIndex := strings.Index(text, "Blocking (1):")
	if appIndex < 0 || instanceIndex <= appIndex || warningsIndex <= instanceIndex || blockingIndex <= warningsIndex {
		t.Fatalf("app/instance review hierarchy is out of order:\n%s", text)
	}
}

func TestAppServiceOverviewRowsGroupsDifferencesByEnvironmentType(t *testing.T) {
	enabled := ServicePlan{SourceName: "php", TargetName: "php", Enabled: true, Action: "migrate"}
	disabled := enabled
	disabled.Enabled = false
	app := AppPlan{Instances: []InstancePlan{
		{Name: "dev-a", Title: "Dev A", TargetEnvType: "DEV", Services: []ServicePlan{enabled}},
		{Name: "dev-b", Title: "Dev B", TargetEnvType: "DEV", Services: []ServicePlan{enabled}},
		{Name: "prod", Title: "Production", TargetEnvType: "PROD", Services: []ServicePlan{disabled}},
	}}

	rows := appServiceOverviewRows(app)
	if len(rows) != 2 {
		t.Fatalf("service overview rows = %#v, want two environment-scoped patterns", rows)
	}
	if got := rows[0][len(rows[0])-1]; got != "DEV instances" {
		t.Fatalf("enabled service scope = %q, want DEV instances", got)
	}
	if got := rows[1][len(rows[1])-1]; got != "PROD instances" {
		t.Fatalf("disabled service scope = %q, want PROD instances", got)
	}
}

func TestPreparedGitRefSummaryShowsEachInstanceWhenRefsDiffer(t *testing.T) {
	devRef, devType := "develop", TargetGitRefBranch
	prodRef, prodType := "v1.2.3", TargetGitRefTag
	prepared := PreparedMigration{Instances: []PreparedInstance{
		{
			Source: Instance{Name: "dev", Title: "Dev"},
			BuildSource: &PreparedBuildSource{Input: TargetBuildSourceInput{
				GitRef: &devRef, GitRefType: &devType,
			}},
		},
		{
			Source: Instance{Name: "prod", Title: "Production"},
			BuildSource: &PreparedBuildSource{Input: TargetBuildSourceInput{
				GitRef: &prodRef, GitRefType: &prodType,
			}},
		},
	}}

	want := `Dev: branch "develop"; Production: tag "v1.2.3"`
	if got := preparedGitRefSummary(prepared); got != want {
		t.Fatalf("prepared Git ref summary = %q, want %q", got, want)
	}
}

func TestPrintReviewColorsSeveritiesWhenForced(t *testing.T) {
	previousNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			_ = os.Setenv("NO_COLOR", previousNoColor)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})
	t.Setenv("CLICOLOR_FORCE", "1")
	plan := Plan{Review: []ReviewItem{
		{Severity: SeverityMigration, Subject: "migration", Message: "do it"},
		{Severity: SeverityBlocking, Subject: "blocked", Message: "fix it"},
		{Severity: SeverityConfirmation, Subject: "warning", Message: "review it"},
		{Severity: SeverityServiceWarning, Subject: "service xhprof", Message: "enabled but not migrated"},
	}}
	var output bytes.Buffer
	PrintReview(&output, plan)
	text := output.String()
	if !strings.Contains(text, ansiRed) || !strings.Contains(text, ansiOrange) ||
		!strings.Contains(text, ansiGreen) || !strings.Contains(text, ansiCyan) ||
		!strings.Contains(text, ansiReset) {
		t.Fatalf("forced color output is incomplete: %q", text)
	}
}

func TestPrintReviewSeparatesMigrationAppAndInstanceScopes(t *testing.T) {
	plan := Plan{
		Apps: []AppPlan{{
			Name: "demo",
			Instances: []InstancePlan{{
				Name: "dev",
			}},
		}},
		Review: []ReviewItem{
			{Severity: SeverityConfirmation, Subject: "global warning", Message: "migration-wide"},
			{Severity: SeverityMigration, App: "demo", Subject: "app change", Message: "shared by instances"},
			{Severity: SeverityBlocking, App: "demo", Subject: "app blocker", Message: "blocks the app"},
			{Severity: SeverityMigration, App: "demo", Instance: "dev", Subject: "instance change", Message: "dev only"},
			{Severity: SeverityConfirmation, App: "demo", Instance: "dev", Subject: "instance warning", Message: "review dev"},
		},
	}

	var output bytes.Buffer
	PrintReview(&output, plan)
	text := output.String()
	for _, expected := range []string{
		"Migration-wide",
		"global warning  migration-wide",
		"App 1/1: demo → demo",
		"app change  shared by instances",
		"app blocker  blocks the app",
		"Instance 1/1: dev → dev",
		"instance change  dev only",
		"instance warning  review dev",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("scoped review does not contain %q:\n%s", expected, text)
		}
	}
	globalIndex := strings.Index(text, "Migration-wide")
	appIndex := strings.Index(text, "App 1/1: demo → demo")
	instanceIndex := strings.Index(text, "Instance 1/1: dev → dev")
	if globalIndex < 0 || appIndex <= globalIndex || instanceIndex <= appIndex {
		t.Fatalf("scope hierarchy is out of order:\n%s", text)
	}
}

func TestPrintReviewPromotesDetailsSharedByEveryInstance(t *testing.T) {
	plan := Plan{
		Apps: []AppPlan{{
			Name: "demo",
			Instances: []InstancePlan{
				{Name: "prod"},
				{Name: "dev"},
			},
		}},
		Review: []ReviewItem{
			{Severity: SeverityMigration, App: "demo", Instance: "prod", Subject: "pipeline", Message: "Wodby CI pipeline found"},
			{Severity: SeverityMigration, App: "demo", Instance: "dev", Subject: "pipeline", Message: "Wodby CI pipeline found"},
			{Severity: SeverityConfirmation, App: "demo", Instance: "prod", Subject: "PHP version", Message: "production differs"},
		},
	}

	var output bytes.Buffer
	PrintReview(&output, plan)
	text := output.String()
	if strings.Count(text, "Wodby CI pipeline found") != 1 || !strings.Contains(text, "All migrated instances: Wodby CI pipeline found") {
		t.Fatalf("common instance detail was not promoted to app scope:\n%s", text)
	}
	appDetail := strings.Index(text, "All migrated instances: Wodby CI pipeline found")
	prodInstance := strings.Index(text, "Instance 1/2: prod → prod")
	if appDetail < 0 || prodInstance <= appDetail {
		t.Fatalf("promoted app detail is not before the instances:\n%s", text)
	}
	if !strings.Contains(text, "production differs") {
		t.Fatalf("instance-specific warning disappeared:\n%s", text)
	}
}

func TestPrintReviewMarksClusterOwnerProjectDefault(t *testing.T) {
	plan := Plan{
		Target: PlanTarget{
			OrgName:     "acme",
			ProjectID:   22,
			ProjectName: "website",
			ClusterName: "production",
		},
	}
	var output bytes.Buffer
	PrintReview(&output, plan)
	if !reviewTableContainsRow(output.String(), "Target project", "website (cluster owner default)") {
		t.Fatalf("inferred project is not identified in review:\n%s", output.String())
	}
}

func reviewTableContainsRow(text, field, value string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, field) {
			continue
		}
		if strings.TrimSpace(strings.TrimPrefix(line, field)) == value {
			return true
		}
	}
	return false
}
