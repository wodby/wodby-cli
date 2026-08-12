package wodby1

import (
	"testing"
	"time"
)

func TestResolveServiceVersionPreservesSupportedSourceVersion(t *testing.T) {
	plan := ServicePlan{SourceName: "php", SourceVersion: "8.3", TargetName: "php"}
	finding, found, err := resolveServiceVersion(
		&plan,
		versionTestInspection("php", []TargetServiceOption{
			{Version: "8.3", Default: true},
			{Version: "8.4"},
		}),
		"",
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if found || finding.Severity != "" || finding.Message != "" {
		t.Fatalf("unexpected finding: %#v", finding)
	}
	if plan.TargetVersion != "8.3" || plan.VersionAction != versionActionPreserve {
		t.Fatalf("unexpected version plan: %#v", plan)
	}
}

func TestResolveServiceVersionUpgradesUnavailableSourceToHighestNonEOL(t *testing.T) {
	plan := ServicePlan{SourceName: "php", SourceVersion: "7.3", TargetName: "php"}
	finding, found, err := resolveServiceVersion(
		&plan,
		versionTestInspection("php", []TargetServiceOption{
			{Version: "8.3", Default: true},
			{Version: "8.4"},
			{Version: "8.5", EOL: time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC)},
		}),
		"",
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || finding.Severity != SeverityConfirmation {
		t.Fatalf("expected important confirmation, got %#v", finding)
	}
	if plan.TargetVersion != "8.4" || plan.VersionAction != versionActionUpgrade {
		t.Fatalf("unexpected version plan: %#v", plan)
	}
}

func TestResolveServiceVersionRejectsDisabledStackOptionOverride(t *testing.T) {
	plan := ServicePlan{
		SourceName: "php", SourceVersion: "8.3", TargetName: "php",
		TargetVersion: "8.4", VersionExplicit: true,
	}
	stackManifest := `{"services":[{"name":"php","options":[{"version":"8.3","default":true},{"version":"8.4","disabled":true}]}]}`
	finding, found, err := resolveServiceVersion(
		&plan,
		versionTestInspection("php", []TargetServiceOption{{Version: "8.3"}, {Version: "8.4"}}),
		stackManifest,
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || finding.Severity != SeverityBlocking {
		t.Fatalf("expected blocking finding, got %#v", finding)
	}
}

func TestResolveServiceVersionUsesStackRevisionDefaultWhenSourceVersionMissing(t *testing.T) {
	plan := ServicePlan{SourceName: "php", TargetName: "php"}
	stackManifest := `{"services":[{"name":"php","options":[{"version":"8.3"},{"version":"8.4","default":true}]}]}`
	finding, found, err := resolveServiceVersion(
		&plan,
		versionTestInspection("php", []TargetServiceOption{
			{Version: "8.3", Default: true},
			{Version: "8.4"},
		}),
		stackManifest,
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || finding.Severity != SeverityConfirmation {
		t.Fatalf("expected important confirmation, got %#v", finding)
	}
	if plan.TargetVersion != "8.4" || plan.VersionAction != versionActionDefault {
		t.Fatalf("unexpected version plan: %#v", plan)
	}
}

func TestCustomStackDefaultEnvironmentRequiresMigration(t *testing.T) {
	if !sourceEnvVarRequiresMigration(nil, EnvVar{Name: "CUSTOM_DEFAULT", Enabled: true, Origin: "custom_stack"}) {
		t.Fatal("custom stack default must be migrated")
	}
	if sourceEnvVarRequiresMigration(nil, EnvVar{Name: "MANAGED_DEFAULT", Enabled: true, Origin: "default"}) {
		t.Fatal("ordinary managed stack default must remain target-owned")
	}
}

func TestTargetServiceCapacityBlocksFreePlanBeforeMutation(t *testing.T) {
	plan := Plan{Target: PlanTarget{Subscription: &TargetOrgSubscription{
		Status: "ACTIVE",
		Plan: &TargetOrgSubscriptionPlan{
			Name: "developer", Usage: 8, UsageIncluded: 10,
		},
	}}}
	prepared := PreparedMigration{Apps: []PreparedAppMigration{{
		Instances: []PreparedInstance{{EffectiveState: map[string]bool{
			"php": true, "nginx": true, "mariadb": true, "mailpit": false,
		}}},
	}}}
	findings := targetServiceCapacityFindings(&plan, prepared, TargetPreflightOptions{})
	if len(findings) != 1 || findings[0].Severity != SeverityBlocking {
		t.Fatalf("expected free-plan capacity blocker, got %#v", findings)
	}
}

func versionTestInspection(name string, options []TargetServiceOption) TargetStackServiceInspection {
	return TargetStackServiceInspection{
		StackService: TargetStackService{Name: name},
		ServiceRevision: TargetServiceRevision{
			Manifest: &TargetServiceManifest{Name: name, Options: options},
		},
	}
}
