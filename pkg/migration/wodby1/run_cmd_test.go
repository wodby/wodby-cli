package wodby1

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWodby1RepositoryMappingUsesNamesInsteadOfProviderIDs(t *testing.T) {
	mappings, err := parseRepositoryMapping([]string{
		"app-1=44",
		"app-2=55:acme/example",
		"app-3=66:acme/other:php",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mappings["app-1"].GitIntegrationID != 44 || mappings["app-1"].RepositoryName != "" ||
		mappings["app-2"].RepositoryName != "acme/example" ||
		mappings["app-3"].RepositoryName != "acme/other" || mappings["app-3"].Service != "php" {
		t.Fatalf("repository mappings = %#v", mappings)
	}
}

func TestConfirmApplyRequiresAnExplicitAnswer(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "yes", input: "yes\n"},
		{name: "short yes", input: "Y\n"},
		{name: "no", input: "no\n", wantErr: true},
		{name: "empty", input: "\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := &RunEnv{}
			var output bytes.Buffer
			cmd.In = strings.NewReader(test.input)
			cmd.Out = &output
			err := confirmApply(cmd, false)
			if (err != nil) != test.wantErr {
				t.Fatalf("confirmApply() error = %v", err)
			}
			if !strings.Contains(output.String(), "Proceed with this migration? [y/N]") {
				t.Fatalf("confirmation prompt missing: %q", output.String())
			}
		})
	}
}

func TestConfirmApplySupportsNonInteractiveApproval(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.In = strings.NewReader("")
	cmd.Out = &output
	if err := confirmApply(cmd, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "[y/N]") || !strings.Contains(output.String(), "approved with --yes") {
		t.Fatalf("non-interactive approval output = %q", output.String())
	}
}

func TestMigrationPreparationProgressShowsCountedAppsAndInstances(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	progress := newMigrationPreparationProgress(cmd, "app")
	for _, title := range []string{
		"Discover Wodby 2 target scope",
		"Export and select Wodby 1 source data",
		"Resolve target environment types",
		"Build migration plan",
		"Inspect target mappings and capabilities",
	} {
		progress.StartStep(title)
		if title == "Inspect target mappings and capabilities" {
			progress.TargetPreflight(TargetPreflightProgress{
				Stage: "app", AppIndex: 2, AppTotal: 4, AppName: "example", InstanceTotal: 3,
			})
			progress.TargetPreflight(TargetPreflightProgress{
				Stage: "instance", AppIndex: 2, AppTotal: 4, AppName: "example",
				InstanceIndex: 1, InstanceTotal: 3, InstanceName: "dev",
			})
			progress.TargetPreflight(TargetPreflightProgress{
				Stage: "app_complete", AppIndex: 2, AppTotal: 4, AppName: "example",
			})
		}
		progress.CompleteStep(title + " completed.")
	}

	text := output.String()
	for _, expected := range []string{
		"App migration preparation",
		"[1/5] Discover Wodby 2 target scope",
		"[5/5] Inspect target mappings and capabilities",
		"App 2/4: example (3 instance(s))",
		"Instance 1/3: dev",
		"App 2/4 inspected.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("preparation progress missing %q:\n%s", expected, text)
		}
	}
}

func TestMigrationPreparationProgressKeepsSingleInstancePreviewCompact(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	progress := newMigrationPreparationProgress(cmd, "instance")
	progress.StartStep("Export source")
	progress.CompleteStep("Export complete")
	progress.TargetPreflight(TargetPreflightProgress{Stage: "instance", InstanceIndex: 1, InstanceTotal: 1})
	if output.Len() != 0 {
		t.Fatalf("single-instance preparation progress should be compact, got %q", output.String())
	}
}

func TestStateTargetInstanceRecoveryAllowsOnlyAmbiguousCreate(t *testing.T) {
	state := &MigrationState{Instances: map[string]*MigrationResourceState{
		"recoverable": {
			Status: MigrationResourceCreating,
			Operations: map[string]MigrationOperationState{
				"create": {Status: MigrationOperationAmbiguous},
			},
		},
		"known": {
			TargetID: 201, Status: MigrationResourceReady,
			Operations: map[string]MigrationOperationState{
				"create": {Status: MigrationOperationSucceeded},
			},
		},
		"failed": {
			Status: MigrationResourceFailed,
			Operations: map[string]MigrationOperationState{
				"create": {Status: MigrationOperationFailed},
			},
		},
	}}
	got := stateTargetInstanceRecovery(state)
	if len(got) != 1 || !got["recoverable"] {
		t.Fatalf("recovery = %#v", got)
	}
}

func TestSingleResumeNoticeExplainsPinnedBackup(t *testing.T) {
	var output bytes.Buffer
	cmd := &RunEnv{}
	cmd.Out = &output
	state := &MigrationState{
		Status: MigrationStatusRunning,
		Phase:  MigrationPhaseSyncData,
	}

	printSingleResumeNotice(cmd, "/tmp/plan.json", "/tmp/state.json", state)

	text := output.String()
	wanted := []string{
		"Resume setup",
		"Step 1/3: Load saved migration",
		"Status: Running",
		"Resume from: Data import",
		"Step 2/3: Select run mode",
		"Mode: continue the saved plan",
		"Step 3/3: Apply command options",
		"Source backups: use the snapshots pinned by the saved plan",
		"Protected download URLs will be refreshed without changing snapshots",
	}
	previous := -1
	for _, item := range wanted {
		index := strings.Index(text, item)
		if index < 0 {
			t.Fatalf("resume notice missing %q:\n%s", item, text)
		}
		if index < previous {
			t.Fatalf("resume notice order is wrong for %q:\n%s", item, text)
		}
		previous = index
	}
	if strings.Contains(text, "/tmp/plan.json") || strings.Contains(text, "/tmp/state.json") {
		t.Fatalf("resume notice exposed artifact paths without --verbose:\n%s", text)
	}
}

func TestSingleResumeNoticeShowsArtifactPathsOnlyWhenVerbose(t *testing.T) {
	var output bytes.Buffer
	cmd := &RunEnv{Verbose: true}
	cmd.Out = &output

	printSingleResumeNotice(cmd, "/tmp/plan.json", "/tmp/state.json", nil)

	text := output.String()
	for _, expected := range []string{
		"Plan file: /tmp/plan.json",
		"State file: /tmp/state.json",
		"Source backups: use the snapshots pinned by the saved plan",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("verbose resume notice missing %q:\n%s", expected, text)
		}
	}
}

func TestMigrationProgressReporterFormatsProcessSteps(t *testing.T) {
	var output bytes.Buffer
	cmd := &RunEnv{}
	cmd.Out = &output
	report := migrationProgressReporter(cmd)

	report("Starting resumable migration apply.")
	report("Preflight: validate and pin the selected source backup before target changes.")
	report("Apply preflight passed; target changes may begin.")
	report("Step: create or resume the target app and app environments.")
	report("Target app created.")

	text := output.String()
	wanted := []string{
		"Migration process",
		"Step 1: validate and pin the selected source backup before target changes",
		"  Apply preflight passed; target changes may begin.",
		"Step 2: create or resume the target app and app environments",
		"  Target app created.",
	}
	previous := -1
	for _, item := range wanted {
		index := strings.Index(text, item)
		if index < 0 || index < previous {
			t.Fatalf("progress output missing or misordered %q:\n%s", item, text)
		}
		previous = index
	}
}

func TestMigrationProgressReporterUsesSemanticColorsWhenForced(t *testing.T) {
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
	var output bytes.Buffer
	cmd := &RunEnv{}
	cmd.Out = &output
	report := migrationProgressReporter(cmd)
	report("Starting resumable migration apply.")
	report("Step: create target resources.")
	report("Target app created.")
	report("Warning: selected backup excludes later writes.")
	text := output.String()
	if !strings.Contains(text, cliColorCyan) || !strings.Contains(text, cliColorGreen) ||
		!strings.Contains(text, cliColorOrange) || !strings.Contains(text, cliColorReset) {
		t.Fatalf("forced progress colors are incomplete: %q", text)
	}
}

func TestPrintPreviewExplainsBlockingNextStep(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	plan := Plan{
		Status:  "blocked",
		Source:  PlanSource{Kind: "instance"},
		Summary: PlanSummary{Blocking: 2},
	}
	if err := printPreview(cmd, plan); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"Next step:",
		"Preview result: blocked",
		"Fix the 2 blocking item(s) above",
		"rerun this preview",
		"This preview made no changes",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("blocked plan output does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "--apply") {
		t.Fatalf("blocked plan suggests applying:\n%s", text)
	}
}

func TestPrintPreviewCountsEnabledSkippedServicesAsWarnings(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	plan := Plan{
		Source:  PlanSource{Kind: "instance"},
		Summary: PlanSummary{ServiceWarnings: 2},
		Review: []ReviewItem{
			{Severity: SeverityServiceWarning, Subject: "service rsyslog", Message: "enabled but not migrated"},
			{Severity: SeverityServiceWarning, Subject: "service xhprof", Message: "enabled but not migrated"},
		},
	}
	if err := printPreview(cmd, plan); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"Enabled services not migrated (2):",
		"No blockers found.",
		"Review the 2 warning(s) above.",
		"Adding --apply acknowledges the warnings above",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("service warning preview does not contain %q:\n%s", expected, text)
		}
	}
}

func TestRejectBlockedMigrationActionReturnsOneActionableError(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	plan := Plan{
		Status:  "blocked",
		Source:  PlanSource{Kind: "instance"},
		Summary: PlanSummary{Blocking: 1},
	}
	err := rejectBlockedMigrationAction(cmd, plan, "apply")
	if err == nil || err.Error() != "cannot apply migration: resolve the 1 blocking review item(s) shown above" {
		t.Fatalf("blocked apply error = %v", err)
	}
	text := output.String()
	if strings.Contains(text, "Migration not started") ||
		strings.Contains(text, "Applying the migration plan") {
		t.Fatalf("blocked apply printed a duplicate status:\n%s", text)
	}
}

func TestServerArtifactPathsRejectPlanAndChildStateCollision(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "server-state.json")
	planPath := serverAppStatePath(statePath, "app-1")
	err := validateServerArtifactPaths(planPath, statePath, Export{
		Apps: []AppExport{{App: App{UUID: "app-1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "per-app migration state path") {
		t.Fatalf("collision error = %v", err)
	}
}

func pausedTestError() *MigrationPausedError {
	return &MigrationPausedError{Actions: []*ExternalActionRequiredError{{
		Instance:         "prod",
		TargetInstanceID: 4100,
		ServiceName:      "php",
		TargetServiceID:  4200,
		ProviderLabel:    "GitHub Actions",
		ExampleURL:       "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
	}}}
}

func TestPausedMigrationPrintsNextStepsInsteadOfAFailure(t *testing.T) {
	silenced := false
	cmd := &RunEnv{SilenceErrors: func() { silenced = true }}
	var output bytes.Buffer
	cmd.Out = &output
	paused := pausedTestError()

	err := printMigrationPaused(cmd, paused, "/tmp/plan.json", "/tmp/state.json")
	if got, ok := AsMigrationPaused(err); !ok || got != paused {
		t.Fatalf("paused runs must still return a non-zero paused error, got %v", err)
	}
	text := output.String()
	for _, want := range []string{
		"Migration paused",
		"https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
		"https://github.com/wodby/wodby-ci/tree/2.0",
		"WODBY_APP_SERVICE_ID  4200",
		"wodby ci deploy",
		"/tmp/state.json",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("paused output missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"failed during", "inspect the target", "migration apply stopped"} {
		if strings.Contains(strings.ToLower(text), unwanted) {
			t.Fatalf("a paused migration must not be framed as a failure (%q):\n%s", unwanted, text)
		}
	}
	// The steps are already printed; the caller must not add usage or repeat the error.
	if !silenced {
		t.Fatal("paused output must suppress the caller's usage and error echo")
	}
}

func TestPausedMigrationJSONExposesTheAppServiceToBuild(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.JSON = true

	_ = printMigrationPaused(cmd, pausedTestError(), "/tmp/plan.json", "/tmp/state.json")

	var decoded migrationPausedOutput
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("json output = %q: %v", output.String(), err)
	}
	if decoded.Status != pausedMigrationStatus || len(decoded.Actions) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if decoded.Actions[0].TargetServiceID != 4200 ||
		decoded.Actions[0].CIProvider != "GitHub Actions" ||
		decoded.Actions[0].WaitsFor != "custom_ci_build" {
		t.Fatalf("action = %#v", decoded.Actions[0])
	}
}

// A --skip-code pause waits for the operator's own code deployment, so its
// status and actions say so instead of asking for a Custom CI build.
func TestPausedMigrationJSONNamesAManualCodeDeployment(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.JSON = true
	paused := &MigrationPausedError{Actions: []*ExternalActionRequiredError{{
		Instance: "prod", TargetInstanceID: 4100, ServiceName: "php", TargetServiceID: 4200,
		ManualCodeDeploy: true, ImportComponent: "files",
	}}}

	_ = printMigrationPaused(cmd, paused, "/tmp/plan.json", "/tmp/state.json")

	var decoded migrationPausedOutput
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("json output = %q: %v", output.String(), err)
	}
	if decoded.Status != pausedCodeDeployStatus || len(decoded.Actions) != 1 ||
		decoded.Actions[0].WaitsFor != "manual_code_deploy" || decoded.Actions[0].ImportComponent != "files" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestRollbackConfirmationRequiresTheAppName(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.In = strings.NewReader("wrong-name\n")
	// A bare y/N is too easy to answer on the wrong terminal for something
	// that deletes imported data.
	if err := confirmRollback(cmd, false, "demo"); err == nil ||
		!strings.Contains(err.Error(), "no Wodby 2 resource was deleted") {
		t.Fatalf("mismatched confirmation returned %v", err)
	}

	cmd.In = strings.NewReader("demo\n")
	if err := confirmRollback(cmd, false, "demo"); err != nil {
		t.Fatalf("typed app name should confirm: %v", err)
	}
}

func TestRollbackJSONOutputRequiresExplicitApproval(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.JSON = true
	if err := confirmRollback(cmd, false, "demo"); err == nil ||
		!strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("non-interactive rollback returned %v", err)
	}
}

func TestRestartConfirmationApprovesCleanupAndFreshApplyOnce(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.In = strings.NewReader("demo\n")

	if err := confirmRestart(cmd, false, "demo", true); err != nil {
		t.Fatalf("typed restart confirmation returned %v", err)
	}
	text := output.String()
	if strings.Count(text, "Type \"demo\"") != 1 ||
		!strings.Contains(text, "delete the resources listed above and apply this fresh migration") ||
		!strings.Contains(text, "Restart and fresh migration approved") {
		t.Fatalf("restart confirmation is unclear:\n%s", text)
	}
}

func TestRestartConfirmationKeepsSavedMigrationOnMismatch(t *testing.T) {
	cmd := &RunEnv{}
	cmd.Out = &bytes.Buffer{}
	cmd.In = strings.NewReader("y\n")

	err := confirmRestart(cmd, false, "demo", true)
	if err == nil || !strings.Contains(err.Error(), "saved plan and state were kept") ||
		!strings.Contains(err.Error(), "no Wodby 2 resource was deleted") {
		t.Fatalf("mismatched restart confirmation returned %v", err)
	}
}

func TestRestartConfirmationWithoutCleanupUsesYesNo(t *testing.T) {
	cmd := &RunEnv{}
	var output bytes.Buffer
	cmd.Out = &output
	cmd.In = strings.NewReader("yes\n")

	if err := confirmRestart(cmd, false, "demo", false); err != nil {
		t.Fatalf("non-destructive restart confirmation returned %v", err)
	}
	if !strings.Contains(output.String(), "Replace the saved plan and state") {
		t.Fatalf("non-destructive restart prompt is unclear:\n%s", output.String())
	}
}

// The plan pins a snapshot for every instance with imports, while the option
// lists only the instances the operator overrode. Rerunning the same command
// must be accepted.
func TestSameSourceBackupSelectionAcceptsPartialRequest(t *testing.T) {
	pinned := SourceBackupSelection{
		"inst-prod":    {"db": "backup-1", "files": "backup-1"},
		"inst-staging": {"db": "backup-9", "files": "backup-9"},
	}
	if !sameSourceBackupSelection(SourceBackupSelection{"inst-staging": {"*": "backup-9"}}, pinned) {
		t.Fatal("the original partial selection must match the pinned snapshots")
	}
	if sameSourceBackupSelection(SourceBackupSelection{"inst-staging": {"*": "backup-2"}}, pinned) {
		t.Fatal("another snapshot must not match")
	}
	if sameSourceBackupSelection(SourceBackupSelection{"inst-dev": {"*": "backup-9"}}, pinned) {
		t.Fatal("an instance the plan did not pin must not match")
	}
}

func TestConfirmRollbackRefusesAnEmptyName(t *testing.T) {
	cmd := &RunEnv{In: strings.NewReader("\n")}
	if err := confirmRollback(cmd, false, ""); err == nil {
		t.Fatal("an empty name must not let Enter approve a rollback")
	}
}

func TestResolveWorkspaceInstances(t *testing.T) {
	export := Export{
		Schema: ExportSchemaV2,
		Source: &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{{
			App:       App{UUID: "app-1", Name: "shop"},
			Instances: []Instance{{UUID: "inst-1", Name: "prod"}, {UUID: "inst-2", Name: "dev"}},
		}},
	}
	for _, tc := range []struct {
		name    string
		kind    string
		opts    RunOptions
		want    map[string]bool
		wantErr bool
	}{
		{name: "none selected", kind: "app", want: map[string]bool{}},
		{name: "by name", kind: "app", opts: RunOptions{WorkspaceInstances: []string{"dev"}}, want: map[string]bool{"inst-2": true}},
		{name: "by UUID and scoped name", kind: "server", opts: RunOptions{WorkspaceInstances: []string{"inst-1", "shop/dev"}}, want: map[string]bool{"inst-1": true, "inst-2": true}},
		{name: "server needs the app", kind: "server", opts: RunOptions{WorkspaceInstances: []string{"dev"}}, wantErr: true},
		{name: "unknown instance", kind: "app", opts: RunOptions{WorkspaceInstances: []string{"stage"}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWorkspaceInstances(export, tc.kind, &tc.opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selected = %#v, want %#v", got, tc.want)
			}
		})
	}

	// An instance migration exports exactly its one instance.
	export.Apps[0].Instances = export.Apps[0].Instances[1:]
	got, err := resolveWorkspaceInstances(export, "instance", &RunOptions{Workspace: true})
	if err != nil || !reflect.DeepEqual(got, map[string]bool{"inst-2": true}) {
		t.Fatalf("selected = %#v, error = %v", got, err)
	}
}
