package wodby1

import (
	"bytes"
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
		},
		Summary: PlanSummary{
			Apps: 1, Instances: 1, Services: 2, Routes: 1,
			Blocking: 1, Confirmation: 1, Manual: 1, Intentionally: 1,
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1",
			Name:       "demo",
			Title:      "Demo",
			Instances: []InstancePlan{{
				Name:       "dev",
				Title:      "Dev",
				SourceType: "dev",
				TargetEnv:  "dev",
				Stack:      StackPlan{Name: "drupal", Target: "drupal11"},
				Services: []ServicePlan{
					{SourceName: "mailhog", TargetName: "mailpit", Enabled: true, Action: "substitute"},
					{SourceName: "xhprof", Enabled: true, Action: "skip"},
				},
				Routes: []RoutePlan{{
					Host: "example.test", Action: "create_backend", Type: "user",
					Service: "nginx", PortNumber: &port, Primary: true,
				}},
			}},
		}},
		Review: []ReviewItem{
			{Severity: SeverityBlocking, App: "demo", Instance: "dev", Subject: "backup", Message: "create a backup"},
			{Severity: SeverityConfirmation, App: "demo", Instance: "dev", Subject: "service mailhog", Message: "use mailpit"},
			{Severity: SeverityManual, App: "demo", Instance: "dev", Subject: "DNS", Message: "switch after verification"},
			{Severity: SeveritySkipped, App: "demo", Instance: "dev", Subject: "service xhprof", Message: "not migrated"},
		},
	}

	var output bytes.Buffer
	PrintReview(&output, plan)
	text := output.String()
	for _, expected := range []string{
		"Field",
		"Value",
		"Item                   Count",
		"Source   Target   State    Action",
		"mailhog  mailpit  enabled  substitute",
		"Host          Action          Type  Service  Port  Flags",
		"Blocking (1):",
		"Requires confirmation (1):",
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
		"Target app":          "demo",
		"Target instance":     "dev",
		"Environment mapping": "dev -> dev",
		"Target stack":        "drupal11",
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
		"Blocking (1):",
		"Requires confirmation (1):",
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
