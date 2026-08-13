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

	var output bytes.Buffer
	PrintReview(&output, plan)
	text := output.String()
	for _, expected := range []string{
		"Field",
		"Value",
		"Item                   Count",
		"Migrations:",
		"App 1/1: Demo → demo",
		"Repository:",
		"connect  Wodby CI (default)  ID 44            acme/demo          exact match found  php",
		"Instance 1/1: Dev → dev (dev → dev)",
		"create and configure  drupal         new from catalog drupal11  revision-4",
		"Source   Target   Source version  Target version  Version action",
		"mailhog  mailpit  -               -               -               enabled  substitute",
		"Cron job → cron schedule migration:",
		"php             php             Drupal cron  0 * * * *  drush cron  disabled by target subscription",
		"Custom domains:",
		"Domain        Action  Target    Target state             Options",
		"example.test  serve   nginx:80  will be created enabled  primary",
		"db         import  mariadb:database  backup-2023-11-14  14 Nov 2023, 22:13 UTC  1.0 MiB",
		"Additional migration details (1):",
		"Warnings (1):",
		"Enabled services not migrated (1):",
		"Blocking (1):",
		"Manual follow-up (1):",
		"Intentionally skipped (1):",
		"Scope     Subject  Details",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("review output does not contain %q:\n%s", expected, text)
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
	if strings.Contains(text, "Review items:") || strings.Contains(text, "[blocking]") {
		t.Fatalf("review output still uses the combined review list:\n%s", text)
	}

	previous := -1
	for _, heading := range []string{
		"Additional migration details (1):",
		"Warnings (1):",
		"Enabled services not migrated (1):",
		"Blocking (1):",
		"Manual follow-up (1):",
		"Intentionally skipped (1):",
	} {
		index := strings.Index(text, heading)
		if index <= previous {
			t.Fatalf("review section %q is out of order:\n%s", heading, text)
		}
		previous = index
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
