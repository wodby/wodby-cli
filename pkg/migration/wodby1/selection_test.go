package wodby1

import (
	"strings"
	"testing"
)

func TestSelectExportFiltersServerAppsAndInstancesAndPinsUUIDs(t *testing.T) {
	export := selectionTestExport("server", "server-1")
	selected, selection, err := SelectExport(export, "server", []string{"second"}, []string{"first/prod"}, "source-token")
	if err != nil {
		t.Fatal(err)
	}
	apps := selected.AppExports()
	if len(apps) != 1 || apps[0].App.UUID != "app-1" || len(apps[0].Instances) != 1 || apps[0].Instances[0].UUID != "inst-dev" {
		t.Fatalf("selected apps = %#v", apps)
	}
	if len(selection.IncludedApps) != 1 || selection.IncludedApps[0].UUID != "app-1" || len(selection.IncludedApps[0].Instances) != 1 || selection.IncludedApps[0].Instances[0] != "inst-dev" {
		t.Fatalf("selection = %#v", selection)
	}
	if len(selection.ExcludedApps) != 1 || selection.ExcludedApps[0].UUID != "app-2" || len(selection.ExcludedInstances) != 1 || selection.ExcludedInstances[0].UUID != "inst-prod" {
		t.Fatalf("exclusions = %#v", selection)
	}
	if selected.ConfigMAC == "" || selected.ConfigMAC == export.ConfigMAC {
		t.Fatalf("selected config MAC = %q", selected.ConfigMAC)
	}
	if len(selected.Issues) != 1 || !strings.Contains(selected.Issues[0].Path, "inst-dev") {
		t.Fatalf("selected issues = %#v", selected.Issues)
	}
}

func TestSelectExportRequiresScopedServerInstanceNames(t *testing.T) {
	_, _, err := SelectExport(selectionTestExport("server", "server-1"), "server", nil, []string{"prod"}, "source-token")
	if err == nil || !strings.Contains(err.Error(), "APP/INSTANCE") {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectExportRejectsEmptySelection(t *testing.T) {
	_, _, err := SelectExport(selectionTestExport("app", "app-1"), "app", nil, []string{"prod", "dev"}, "source-token")
	if err == nil || !strings.Contains(err.Error(), "selection is empty") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplySourceSelectionRejectsMissingReviewedInstance(t *testing.T) {
	export := selectionTestExport("server", "server-1")
	_, selection, err := SelectExport(export, "server", []string{"second"}, []string{"first/prod"}, "source-token")
	if err != nil {
		t.Fatal(err)
	}
	export.Apps[0].Instances = nil
	_, err = ApplySourceSelection(export, selection, "source-token")
	if err == nil || !strings.Contains(err.Error(), "inst-dev") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPlanPersistsSelectionAndReportsExclusions(t *testing.T) {
	export := selectionTestExport("server", "server-1")
	selected, selection, err := SelectExport(export, "server", []string{"second"}, []string{"first/prod"}, "source-token")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(selected, PlanOptions{SourceKind: "server", SourceID: "server-1", Selection: &selection, SkipCode: true, SkipData: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selection.IncludedApps) != 1 || plan.Summary.Intentionally < 2 {
		t.Fatalf("plan selection = %#v, summary = %#v, review = %#v", plan.Selection, plan.Summary, plan.Review)
	}
}

func selectionTestExport(kind, sourceID string) Export {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: kind, UUID: sourceID},
		Apps: []AppExport{
			{
				App: App{UUID: "app-1", Name: "first", Title: "First"},
				Instances: []Instance{
					{UUID: "inst-prod", Name: "prod", Title: "Production", Type: "prod", Stack: Stack{Name: "drupal10"}},
					{UUID: "inst-dev", Name: "dev", Title: "Development", Type: "dev", Stack: Stack{Name: "drupal10"}},
				},
			},
			{
				App: App{UUID: "app-2", Name: "second", Title: "Second"},
				Instances: []Instance{
					{UUID: "inst-second", Name: "prod", Title: "Production", Type: "prod", Stack: Stack{Name: "wordpress"}},
				},
			},
		},
		Issues: []ExportIssue{
			{Code: "included", Severity: SeverityManual, Path: "apps.app-1.instances.inst-dev", Message: "included"},
			{Code: "excluded-instance", Severity: SeverityManual, Path: "apps.app-1.instances.inst-prod", Message: "excluded"},
			{Code: "excluded-app", Severity: SeverityManual, Path: "apps.app-2.instances.inst-second", Message: "excluded"},
		},
	}
	if kind != "server" {
		export.Apps = export.Apps[:1]
	}
	return export
}
