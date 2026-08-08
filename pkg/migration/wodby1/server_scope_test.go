package wodby1

import (
	"strings"
	"testing"
)

func TestScopeServerMigrationAppCreatesIndependentExecutableArtifacts(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Source = &ExportSource{Kind: "server", UUID: "server-1"}
	second := export.Apps[0]
	second.Instances = append([]Instance(nil), second.Instances...)
	second.App.UUID = "app-2"
	second.App.Name = "second"
	second.App.Title = "Second"
	second.Instances[0].UUID = "inst-2"
	export.Apps = append(export.Apps, second)

	options := preflightOwnerPlanOptions()
	options.SourceKind = "server"
	options.SourceID = "server-1"
	options.SkipCode = true
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	if err := plan.AddReviewItems(
		ReviewItem{Severity: SeverityConfirmation, Subject: "global", Message: "global review"},
		ReviewItem{Severity: SeverityManual, App: "example", Subject: "first", Message: "first review"},
		ReviewItem{Severity: SeverityManual, App: "second", Subject: "second", Message: "second review"},
	); err != nil {
		t.Fatal(err)
	}
	prepared := PreparedMigration{Apps: []PreparedAppMigration{
		{App: export.Apps[0], Instances: []PreparedInstance{{Source: export.Apps[0].Instances[0]}}},
		{App: export.Apps[1], Instances: []PreparedInstance{{Source: export.Apps[1].Instances[0]}}},
	}}

	childExport, childPlan, childPrepared, err := ScopeServerMigrationApp(
		export,
		plan,
		prepared,
		"app-1",
		strings.Repeat("k", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := childExport.ValidateSource("app", "app-1"); err != nil {
		t.Fatal(err)
	}
	if err := childPlan.ValidateReviewed(); err != nil {
		t.Fatal(err)
	}
	if len(childPlan.Apps) != 1 || childPlan.Apps[0].SourceUUID != "app-1" ||
		childPrepared.App.App.UUID != "app-1" || len(childPrepared.Instances) != 1 {
		t.Fatalf("scoped migration = export %#v plan %#v prepared %#v", childExport.Source, childPlan.Apps, childPrepared)
	}
	for _, item := range childPlan.Review {
		if item.App == "second" {
			t.Fatalf("child plan retained another app's review: %#v", childPlan.Review)
		}
	}

	changed := export
	changed.Apps = append([]AppExport(nil), export.Apps...)
	changed.Apps[1].App.Title = "Changed second app"
	changedPlan := preflightBuildPlan(t, changed, options)
	if err := changedPlan.AddReviewItems(
		ReviewItem{Severity: SeverityConfirmation, Subject: "global", Message: "global review"},
		ReviewItem{Severity: SeverityManual, App: "example", Subject: "first", Message: "first review"},
		ReviewItem{Severity: SeverityManual, App: "second", Subject: "second", Message: "second review"},
	); err != nil {
		t.Fatal(err)
	}
	_, unchangedChildPlan, _, err := ScopeServerMigrationApp(
		changed,
		changedPlan,
		PreparedMigration{},
		"app-1",
		strings.Repeat("k", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	if unchangedChildPlan.PlanHash != childPlan.PlanHash ||
		unchangedChildPlan.Source.ConfigDigest != childPlan.Source.ConfigDigest {
		t.Fatal("another server app changed the first app's resume identity")
	}
}
