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
	if !sourceEnvVarRequiresMigration(nil, EnvVar{Name: "CUSTOM_DEFAULT", Value: "on", Enabled: true, Origin: "custom_stack"}) {
		t.Fatal("custom stack default must be migrated")
	}
	if sourceEnvVarRequiresMigration(nil, EnvVar{Name: "MANAGED_DEFAULT", Enabled: true, Origin: "default"}) {
		t.Fatal("ordinary managed stack default must remain target-owned")
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

// The PHP SSH derivative's revision is the PHP service's, with PHP's version
// options, but Wodby 2 runs it with its parent's version and stores none.
func derivativeVersionTestInspection() TargetStackServiceInspection {
	inspection := versionTestInspection("sshd", []TargetServiceOption{{Version: "8.3"}, {Version: "8.4", Default: true}})
	inspection.StackService.Type = "ssh"
	inspection.ServiceRevision.Type = "php"
	return inspection
}

func TestResolveServiceVersionLeavesDerivativeWithoutVersion(t *testing.T) {
	plan := ServicePlan{SourceName: "sshd", SourceVersion: "8.4", TargetName: "sshd"}
	finding, found, err := resolveServiceVersion(
		&plan,
		derivativeVersionTestInspection(),
		"",
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if found || finding.Severity != "" {
		t.Fatalf("unexpected finding: %#v", finding)
	}
	if plan.TargetVersion != "" || plan.VersionAction != "" {
		t.Fatalf("a derivative got a version of its own: %#v", plan)
	}
}

func TestResolveServiceVersionRejectsExplicitDerivativeVersion(t *testing.T) {
	plan := ServicePlan{SourceName: "sshd", TargetName: "sshd", TargetVersion: "8.4", VersionExplicit: true}
	finding, found, err := resolveServiceVersion(
		&plan,
		derivativeVersionTestInspection(),
		"",
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || finding.Severity != SeverityBlocking {
		t.Fatalf("expected blocking finding, got %#v", finding)
	}
	if plan.TargetVersion != "" {
		t.Fatalf("a derivative kept the requested version: %#v", plan)
	}
}

func TestVerifyServiceVersion(t *testing.T) {
	parent := 1
	for _, tc := range []struct {
		name    string
		service TargetAppService
		want    string
		fails   bool
	}{
		{name: "matching version", service: TargetAppService{Name: "php", Version: "8.4"}, want: "8.4"},
		{name: "other version", service: TargetAppService{Name: "php", Version: "8.3"}, want: "8.4", fails: true},
		{name: "no version planned", service: TargetAppService{Name: "php", Version: "8.3"}},
		// A plan made before derivatives were left out of version selection.
		{name: "derivative", service: TargetAppService{Name: "sshd", ParentAppServiceID: &parent}, want: "8.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyServiceVersion(tc.service, PreparedService{TargetVersion: tc.want})
			if (err != nil) != tc.fails {
				t.Fatalf("verifyServiceVersion() error = %v, want failure %v", err, tc.fails)
			}
		})
	}
}
