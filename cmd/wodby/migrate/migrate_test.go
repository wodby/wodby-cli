package migrate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

const testSourceToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func TestRetryAmbiguousFlagRequiresAnExactOperationID(t *testing.T) {
	cmd := newWodby1AppCommand()
	flag := cmd.Flags().Lookup("retry-ambiguous")
	if flag == nil {
		t.Fatal("--retry-ambiguous flag is missing")
	}
	if got := flag.Value.Type(); got != "string" {
		t.Fatalf("--retry-ambiguous type = %q, want string", got)
	}
	if err := cmd.Flags().Set("retry-ambiguous", "instance:source-uuid:route.0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if got, err := cmd.Flags().GetString("retry-ambiguous"); err != nil ||
		got != "instance:source-uuid:route.0123456789abcdef" {
		t.Fatalf("--retry-ambiguous = %q, %v", got, err)
	}
}

func TestWodby1AppCommandExamplesIncludeAPIKeyEnvironment(t *testing.T) {
	examples := newWodby1AppCommand().Example
	for _, expected := range []string{
		"export WODBY1_SOURCE_TOKEN=...",
		"export WODBY_API_KEY=...",
	} {
		if !strings.Contains(examples, expected) {
			t.Fatalf("examples do not contain %q:\n%s", expected, examples)
		}
	}
}

func TestWodby1InstanceCommandExamplesIdentifyCredentialsAndWorkflow(t *testing.T) {
	examples := newWodby1InstanceCommand().Example
	for _, required := range []string{
		"WODBY1_SOURCE_TOKEN",
		"WODBY_API_KEY",
		"wodby migrate wodby1 instance INSTANCE_UUID --target-cluster CLUSTER",
		"--apply",
		"--verify",
	} {
		if !strings.Contains(examples, required) {
			t.Fatalf("examples missing %q:\n%s", required, examples)
		}
	}
}

func TestWodby1RepositorySelectionUsesNamesInsteadOfProviderIDs(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	if cmd.Flags().Lookup("target-git-integration-id") == nil {
		t.Fatal("--target-git-integration-id flag is missing")
	}
	if cmd.Flags().Lookup("target-repository-name") == nil {
		t.Fatal("--target-repository-name flag is missing")
	}
	if cmd.Flags().Lookup("target-remote-git-repo-id") != nil {
		t.Fatal("provider repository ID must not be a customer-facing migration flag")
	}

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

func TestWodby1MigrationDefaultsToBuiltInWodbyCI(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	flag := cmd.Flags().Lookup("target-ci-integration-id")
	if flag == nil {
		t.Fatal("--target-ci-integration-id flag is missing")
	}
	if flag.DefValue != "0" {
		t.Fatalf("--target-ci-integration-id default = %q, want 0 for Wodby CI", flag.DefValue)
	}
}

func TestWodby1MigrationExposesPreviewApplyVerifyWorkflow(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	for _, name := range []string{"apply", "verify", "restart"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("--%s flag is missing", name)
		}
	}
	for _, name := range []string{"phase", "approve-plan", "plan-file"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("legacy --%s flag must not be customer-facing", name)
		}
	}
	force := cmd.Flags().Lookup("force")
	if force == nil || !strings.Contains(force.Usage, "without maintenance mode") {
		t.Fatal("--force must explicitly describe its limited live-source behavior")
	}
}

func TestWodby1MigrationExclusionFlagsMatchCommandScope(t *testing.T) {
	instance := newWodby1InstanceCommand()
	if instance.Flags().Lookup("exclude-app") != nil || instance.Flags().Lookup("exclude-instance") != nil {
		t.Fatal("instance migration must not expose exclusion flags")
	}

	app := newWodby1AppCommand()
	if app.Flags().Lookup("exclude-app") != nil || app.Flags().Lookup("exclude-instance") == nil {
		t.Fatal("app migration must expose only --exclude-instance")
	}

	server := newWodby1ServerCommand()
	if server.Flags().Lookup("exclude-app") == nil || server.Flags().Lookup("exclude-instance") == nil {
		t.Fatal("server migration must expose app and instance exclusions")
	}
}

func TestWodby1MigrationExposesUnsupportedDrupalOverride(t *testing.T) {
	flag := newWodby1AppCommand().Flags().Lookup("allow-unsupported-drupal")
	if flag == nil || !strings.Contains(flag.Usage, "Drupal 10 or newer") {
		t.Fatalf("--allow-unsupported-drupal flag = %#v", flag)
	}
}

func TestWodby1MigrationShowsUsageOnlyForCommandErrors(t *testing.T) {
	t.Run("invalid arguments show usage", func(t *testing.T) {
		var output bytes.Buffer
		cmd := newWodby1InstanceCommand()
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(nil)

		if err := cmd.Execute(); err == nil {
			t.Fatal("expected missing source argument to fail")
		}
		if !strings.Contains(output.String(), "Usage:") {
			t.Fatalf("command error did not show usage:\n%s", output.String())
		}
	})

	t.Run("migration blockers suppress usage", func(t *testing.T) {
		fixture := newMigrationAPIFixture(t, "owner", "ok", false)
		defer fixture.Close()
		fixture.setSourceKind("instance")
		setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

		var output bytes.Buffer
		cmd := newWodby1InstanceCommand()
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		args := fixture.instancePlanArgs("", "text")
		args = append(args, "--skip-data=false", "--apply")
		cmd.SetArgs(args)

		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "blocking review item") {
			t.Fatalf("error = %v, want migration blocker", err)
		}
		if strings.Contains(output.String(), "Usage:") {
			t.Fatalf("migration blocker unexpectedly showed usage:\n%s", output.String())
		}
	})
}

func TestWodby1MigrationRecordsSelectedThirdPartyCI(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "owner", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("instance")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	var output bytes.Buffer
	cmd := newWodby1InstanceCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	args := fixture.instancePlanArgs("", "json")
	args = append(args, "--target-ci-integration-id", "77")
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var plan wodby1.Plan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Target.CIIntegrationID != 77 {
		t.Fatalf("target CI integration ID = %d, want 77", plan.Target.CIIntegrationID)
	}
}

func TestWodby1InstanceCommandPlansOnlyRequestedInstance(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "owner", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("instance")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	var output bytes.Buffer
	cmd := newWodby1InstanceCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	cmd.SetArgs(fixture.instancePlanArgs("", "json"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var plan wodby1.Plan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Source.Kind != "instance" || plan.Source.ID != "instance-1" ||
		len(plan.Apps) != 1 || len(plan.Apps[0].Instances) != 1 ||
		plan.Apps[0].Instances[0].SourceUUID != "instance-1" {
		t.Fatalf("instance plan = %#v", plan)
	}
	if got := fixture.sourceRequestPaths(); len(got) != 1 ||
		got[0] != "GET /api/v4/migrations/v2/instances/instance-1/export" {
		t.Fatalf("source requests = %#v", got)
	}
}

func TestWodby1ServerCommandPlansEverySourceApp(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "owner", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	var output bytes.Buffer
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	cmd.SetArgs(fixture.serverPlanArgs("", "json"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var plan wodby1.Plan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Source.Kind != "server" || plan.Source.ID != "server-1" ||
		len(plan.Apps) != 2 || plan.Summary.Apps != 2 || plan.Summary.Instances != 2 {
		t.Fatalf("server plan = %#v", plan)
	}
	if got := fixture.sourceRequestPaths(); len(got) != 1 ||
		got[0] != "GET /api/v4/migrations/v2/servers/server-1/export" {
		t.Fatalf("source requests = %#v", got)
	}
}

func TestWodby1ServerMutationUsesPerAppResumeState(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	statePath := filepath.Join(dir, "server-state.json")

	args := fixture.serverPlanArgs("", "text")
	args = append(args,
		"--state-file", statePath,
		"--apply",
	)
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "migrate source app demo (app-1)") {
		t.Fatalf("prepare error = %v", err)
	}

	childStatePath := serverAppStatePath(statePath, "app-1")
	data, readErr := os.ReadFile(childStatePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var state wodby1.MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Source.Kind != "app" || state.Source.ID != "app-1" {
		t.Fatalf("child state source = %#v", state.Source)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("aggregate state file should not be written: %v", err)
	}
}

func TestWodby1ServerResumePreservesSavedPlanAndExplainsContinuation(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "server-state.json")
	args := append(fixture.serverPlanArgs("", "text"),
		"--state-file", statePath,
		"--apply",
	)

	first := newWodby1ServerCommand()
	first.SilenceUsage = true
	first.SetOut(&bytes.Buffer{})
	first.SetArgs(args)
	if err := first.Execute(); err == nil || !strings.Contains(err.Error(), "migrate source app demo") {
		t.Fatalf("first server apply error = %v", err)
	}
	planPath, _, err := artifactPaths("server", "server-1", statePath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	resume := newWodby1ServerCommand()
	resume.SilenceUsage = true
	resume.SetOut(&output)
	resume.SetArgs(args)
	if err := resume.Execute(); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("server resume error = %v", err)
	}
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("server resume overwrote the saved applied plan")
	}
	text := output.String()
	if !strings.Contains(text, "Server migration resume setup") ||
		!strings.Contains(text, "Step 1/3: Load saved migration") ||
		!strings.Contains(text, "Saved server plan: found") ||
		!strings.Contains(text, "Mode: continue the saved server plan") ||
		!strings.Contains(text, "Continuing the saved migration plan shown above") {
		t.Fatalf("server resume output is unclear:\n%s", text)
	}
	if strings.Contains(text, planPath) || strings.Contains(text, statePath) {
		t.Fatalf("server resume exposed artifact paths without --verbose:\n%s", text)
	}
}

func TestWodby1ServerFreshApplyReplacesStalePlan(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "server-state.json")
	planPath, _, err := artifactPaths("server", "server-1", statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte("stale plan\n"), 0600); err != nil {
		t.Fatal(err)
	}

	args := append(fixture.serverPlanArgs("", "text"),
		"--state-file", statePath,
		"--apply",
	)
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "migrate source app demo (app-1)") {
		t.Fatalf("apply error = %v", err)
	}

	plan, err := wodby1.LoadReviewedPlan(planPath)
	if err != nil {
		t.Fatalf("load replaced plan: %v", err)
	}
	if plan.Source.Kind != "server" || plan.Source.ID != "server-1" {
		t.Fatalf("replaced plan source = %#v", plan.Source)
	}
}

func TestWodby1ServerRestartReplansAfterDefinitiveRejection(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "server-state.json")

	var preview bytes.Buffer
	previewCmd := newWodby1ServerCommand()
	previewCmd.SilenceUsage = true
	previewCmd.SetOut(&preview)
	previewCmd.SetArgs(fixture.serverPlanArgs("", "json"))
	if err := previewCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var oldPlan wodby1.Plan
	if err := json.Unmarshal(preview.Bytes(), &oldPlan); err != nil {
		t.Fatal(err)
	}
	planPath, _, err := artifactPaths("server", "server-1", statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := writePlanFile(planPath, oldPlan); err != nil {
		t.Fatal(err)
	}
	childStatePath := serverAppStatePath(statePath, "app-1")
	saveDefinitivelyRejectedState(t, childStatePath, wodby1.MigrationStateIdentity{
		Source: wodby1.MigrationStateSourceIdentity{
			Kind: "app", ID: "app-1", ConfigDigest: strings.Repeat("a", 64),
		},
		PlanHash: strings.Repeat("b", 64),
		Target: wodby1.MigrationStateTarget{
			OrgID: oldPlan.Target.OrgID, ProjectID: oldPlan.Target.ProjectID, ClusterID: oldPlan.Target.ClusterID,
		},
	}, []string{"instance-1"})
	fixture.setSourceTitle("Changed after rejection")

	args := append(fixture.serverPlanArgs("", "text"),
		"--state-file", statePath,
		"--apply",
		"--restart",
	)
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	applyErr := cmd.Execute()
	if applyErr == nil || !strings.Contains(applyErr.Error(), "migrate source app demo (app-1)") ||
		strings.Contains(applyErr.Error(), "no longer match the applied plan") {
		t.Fatalf("apply error = %v", applyErr)
	}
	newPlan, err := wodby1.LoadReviewedPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if newPlan.PlanHash == oldPlan.PlanHash {
		t.Fatal("definitively rejected server migration kept the stale plan")
	}
}

func TestWodby1ServerResumeStateRequiresSavedPlan(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "server-state.json")
	childStatePath := serverAppStatePath(statePath, "app-1")
	if err := os.WriteFile(childStatePath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	args := append(fixture.serverPlanArgs("", "text"),
		"--state-file", statePath,
		"--apply",
	)
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "server migration state exists") ||
		!strings.Contains(err.Error(), "plan is missing") {
		t.Fatalf("apply error = %v", err)
	}
	if got := fixture.sourceRequestPaths(); len(got) != 0 {
		t.Fatalf("source requests = %#v", got)
	}
}

func TestWodby1AppCommandRequiresTargetClusterAndCredentials(t *testing.T) {
	if flag := newWodby1AppCommand().Flags().Lookup("target-org"); flag != nil {
		t.Fatal("--target-org must not be exposed; the API key selects the organization")
	}

	tests := []struct {
		name       string
		project    string
		cluster    string
		apiBaseURL string
		apiKey     string
		access     string
		want       string
	}{
		{
			name: "cluster", project: "22",
			apiBaseURL: "https://target.example.test/v1", apiKey: "key",
			want: "--target-cluster is required",
		},
		{
			name: "target API base URL", project: "22", cluster: "33",
			apiKey: "key",
			want:   "--api-base-url is required",
		},
		{
			name: "target API credentials", project: "22", cluster: "33",
			apiBaseURL: "https://target.example.test/v1",
			want:       "--api-key is required",
		},
		{
			name: "target access token is not sufficient", project: "22", cluster: "33",
			apiBaseURL: "https://target.example.test/v1", access: "ci-token",
			want: "--api-key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMigrationTargetConfig(t, tt.apiBaseURL, tt.apiKey, tt.access)
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{
				"app-1",
				"--source-token", testSourceToken,
				"--target-project", tt.project,
				"--target-cluster", tt.cluster,
			})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
		})
	}
}

func TestWodby1AppCommandUsesSelectedOrgOwnerOrAdminRole(t *testing.T) {
	for _, role := range []string{"owner", "admin"} {
		t.Run(role, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, role, "ok", false)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

			var output bytes.Buffer
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetOut(&output)
			cmd.SetArgs(fixture.planArgs("", "json"))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			var plan wodby1.Plan
			if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if !plan.Target.OrgOwnerOrAdminVerified || plan.Target.OrgRole != role {
				t.Fatalf("target authorization = %#v", plan.Target)
			}
			if fixture.sourceRequestCount() != 1 {
				t.Fatalf("source requests = %d, want 1", fixture.sourceRequestCount())
			}
		})
	}
}

func TestWodby1AppCommandIgnoresPlatformAdminAndAuthorizesBeforeSecretExport(t *testing.T) {
	tests := []struct {
		name   string
		role   string
		status string
	}{
		{name: "platform admin is only an org member", role: "member", status: "ok"},
		{name: "org admin membership is inactive", role: "admin", status: "invited"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, tt.role, tt.status, true)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetArgs(fixture.planArgs("", "text"))
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "organization OWNER or ADMIN credentials are required") {
				t.Fatalf("error = %v", err)
			}
			if fixture.sourceRequestCount() != 0 {
				t.Fatal("source secret export was requested before target organization authorization")
			}
			if got := fixture.targetRequestPaths(); !containsMigrationRequest(got, "GET /v1/org-memberships?orgId=11") {
				t.Fatalf("target requests = %#v", got)
			}
		})
	}
}

func TestWodby1AppCommandAutomaticallyRequestsSecretsAndHasNoSecretOptInFlags(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv(sourceTokenEnv, testSourceToken)

	cmd := newWodby1AppCommand()
	for _, flag := range []string{"include-secrets", "allow-missing-secrets"} {
		if cmd.Flags().Lookup(flag) != nil {
			t.Fatalf("customer migration unexpectedly exposes --%s", flag)
		}
	}
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	args := fixture.planArgs("", "json")
	args = removeMigrationFlag(args, "--source-token")
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	requests := fixture.sourceRequestPaths()
	if len(requests) != 1 ||
		requests[0] != "GET /api/v4/migrations/v2/apps/app-1/export" {
		t.Fatalf("source requests = %#v", requests)
	}
	if got := fixture.sourceAPIKey(); got != testSourceToken {
		t.Fatalf("source X-API-Key = %q", got)
	}
}

func TestWodby1AppCommandPreviewsWithoutWritingArtifacts(t *testing.T) {
	tests := []struct {
		output string
		check  func(*testing.T, []byte)
	}{
		{
			output: "text",
			check: func(t *testing.T, output []byte) {
				t.Helper()
				if !bytes.Contains(output, []byte("Wodby 1 to Wodby 2 migration plan")) ||
					!bytes.Contains(output, []byte("Target CI")) ||
					!bytes.Contains(output, []byte("Wodby CI (built-in)")) ||
					!bytes.Contains(output, []byte("Next step:")) ||
					!bytes.Contains(output, []byte("No blockers found.")) ||
					!bytes.Contains(output, []byte("same command with --apply")) ||
					!bytes.Contains(output, []byte("did not create a plan, state, or lock file")) {
					t.Fatalf("text output = %s", output)
				}
			},
		},
		{
			output: "json",
			check: func(t *testing.T, output []byte) {
				t.Helper()
				var plan wodby1.Plan
				if err := json.Unmarshal(output, &plan); err != nil {
					t.Fatalf("decode JSON output: %v\n%s", err, output)
				}
				if plan.Schema != wodby1.MigrationPlanSchema || len(plan.PlanHash) != 64 {
					t.Fatalf("JSON plan = %#v", plan)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, "owner", "ok", false)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			var output bytes.Buffer
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetOut(&output)
			cmd.SetArgs(fixture.planArgs("", tt.output))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			tt.check(t, output.Bytes())

			if _, err := os.Stat(filepath.Join(tempDir, "wodby-migrations")); !os.IsNotExist(err) {
				t.Fatalf("preview created temporary migration artifacts: %v", err)
			}
			if tt.output == "json" {
				var plan wodby1.Plan
				if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
					t.Fatal(err)
				}
				if plan.Target.OrgID != 11 || plan.Target.ProjectID != 22 ||
					plan.Target.ClusterID != 33 || !plan.Target.DiscoveryVerified ||
					!plan.Target.OrgOwnerOrAdminVerified {
					t.Fatalf("target plan = %#v", plan.Target)
				}
				instance := plan.Apps[0].Instances[0]
				if instance.TargetEnvID != 44 || instance.Stack.Target != "acme/drupal11" ||
					instance.Stack.TargetID != 7 || instance.Stack.TargetRevID != 71 {
					t.Fatalf("preflighted instance = %#v", instance)
				}
			}

			requests := fixture.targetRequestPaths()
			for _, want := range []string{
				"GET /v1/orgs",
				"GET /v1/user",
				"GET /v1/org-memberships?orgId=11",
				"GET /v1/projects/22",
				"GET /v1/clusters/33",
				"GET /v1/clusters?orgId=11&projectIds=22",
				"GET /v1/envs?orgId=11",
				"GET /v1/stacks/7",
				"GET /v1/stack-revisions/71/services",
			} {
				if !containsMigrationRequest(requests, want) {
					t.Fatalf("target requests = %#v, missing %q", requests, want)
				}
			}
		})
	}
}

func TestWodby1AppCommandApplyWritesTemporaryPlanAndShowsPaths(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	var output bytes.Buffer
	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	args := append(fixture.planArgs("", "text"), "--apply")
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("apply error = %v", err)
	}
	planPath, statePath, pathErr := artifactPaths("app", "app-1", "")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	for _, path := range []string{planPath, statePath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("permissions for %s = %o, want 600", path, got)
		}
	}
	text := output.String()
	for _, expected := range []string{
		"Applying the migration plan shown above",
		"Temporary plan file: " + planPath,
		"Temporary resume-state file: " + statePath,
		"Starting migration",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("apply output missing %q:\n%s", expected, text)
		}
	}
}

func TestWodby1AppCommandResumePreservesSavedPlanAndExplainsContinuation(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	args := append(fixture.planArgs("", "text"), "--apply")

	first := newWodby1AppCommand()
	first.SilenceUsage = true
	first.SetOut(&bytes.Buffer{})
	first.SetArgs(args)
	if err := first.Execute(); err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("first apply error = %v", err)
	}
	planPath, statePath, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	resume := newWodby1AppCommand()
	resume.SilenceUsage = true
	resume.SetOut(&output)
	resume.SetArgs(args)
	if err := resume.Execute(); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resume error = %v", err)
	}
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("resume overwrote the saved applied plan")
	}
	text := output.String()
	for _, expected := range []string{
		"Resume setup",
		"Step 1/3: Load saved migration",
		"Saved plan: found",
		"Resume state: found",
		"Mode: continue the saved plan",
		"Continuing the saved migration plan shown above",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("resume output missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, planPath) || strings.Contains(text, statePath) {
		t.Fatalf("resume exposed artifact paths without --verbose:\n%s", text)
	}
}

func TestSingleResumeNoticeExplainsForceOverride(t *testing.T) {
	previousVerbose := viper.GetBool("verbose")
	viper.Set("verbose", false)
	t.Cleanup(func() { viper.Set("verbose", previousVerbose) })
	var output bytes.Buffer
	cmd := newWodby1AppCommand()
	cmd.SetOut(&output)
	state := &wodby1.MigrationState{
		Status: wodby1.MigrationStatusRunning,
		Phase:  wodby1.MigrationPhaseSyncData,
	}

	printSingleResumeNotice(cmd, "/tmp/plan.json", "/tmp/state.json", state, true)

	text := output.String()
	wanted := []string{
		"Resume setup",
		"Step 1/3: Load saved migration",
		"Status: Running",
		"Resume from: Data import",
		"Step 2/3: Select run mode",
		"Mode: continue the saved plan",
		"Step 3/3: Apply command options",
		"--force: enabled",
		"Maintenance-mode and backup-age requirements will be bypassed",
		"A completed backup is still required; writes made after it are not included",
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
	previousVerbose := viper.GetBool("verbose")
	viper.Set("verbose", true)
	t.Cleanup(func() { viper.Set("verbose", previousVerbose) })
	var output bytes.Buffer
	cmd := newWodby1AppCommand()
	cmd.SetOut(&output)

	printSingleResumeNotice(cmd, "/tmp/plan.json", "/tmp/state.json", nil, false)

	text := output.String()
	for _, expected := range []string{
		"Plan file: /tmp/plan.json",
		"State file: /tmp/state.json",
		"--force: disabled",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("verbose resume notice missing %q:\n%s", expected, text)
		}
	}
}

func TestMigrationProgressReporterFormatsProcessSteps(t *testing.T) {
	var output bytes.Buffer
	cmd := newWodby1AppCommand()
	cmd.SetOut(&output)
	report := migrationProgressReporter(cmd)

	report("Starting resumable migration apply.")
	report("Preflight: validate the existing source backup before target changes (--force allows post-backup writes to be excluded).")
	report("Apply preflight passed; target changes may begin.")
	report("Step: create or resume the target app and app instances.")
	report("Target app created.")

	text := output.String()
	wanted := []string{
		"Migration process",
		"Step 1: validate the existing source backup before target changes",
		"  Apply preflight passed; target changes may begin.",
		"Step 2: create or resume the target app and app instances",
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
	cmd := newWodby1AppCommand()
	cmd.SetOut(&output)
	report := migrationProgressReporter(cmd)
	report("Starting resumable migration apply.")
	report("Step: create target resources.")
	report("Target app created.")
	report("Warning: --force bypass is enabled.")
	text := output.String()
	if !strings.Contains(text, cliColorCyan) || !strings.Contains(text, cliColorGreen) ||
		!strings.Contains(text, cliColorOrange) || !strings.Contains(text, cliColorReset) {
		t.Fatalf("forced progress colors are incomplete: %q", text)
	}
}

func TestWodby1AppCommandRestartRejectsStateWithTargetMutationRisk(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	args := append(fixture.planArgs("", "text"), "--apply")

	first := newWodby1AppCommand()
	first.SilenceUsage = true
	first.SetOut(&bytes.Buffer{})
	first.SetArgs(args)
	if err := first.Execute(); err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("first apply error = %v", err)
	}
	sourceRequests := fixture.sourceRequestCount()
	targetRequests := len(fixture.targetRequestPaths())

	restart := newWodby1AppCommand()
	restart.SilenceUsage = true
	restart.SetOut(&bytes.Buffer{})
	restart.SetArgs(append(args, "--restart"))
	err := restart.Execute()
	if err == nil || !strings.Contains(err.Error(), "records target mutations") ||
		!strings.Contains(err.Error(), "continue without --restart") {
		t.Fatalf("restart error = %v", err)
	}
	if fixture.sourceRequestCount() != sourceRequests || len(fixture.targetRequestPaths()) != targetRequests {
		t.Fatal("unsafe restart made API requests before rejecting persisted mutation risk")
	}
}

func TestWodby1AppCommandRestartsAfterSavedTargetWasDeleted(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())

	var preview bytes.Buffer
	previewCmd := newWodby1AppCommand()
	previewCmd.SilenceUsage = true
	previewCmd.SetOut(&preview)
	previewCmd.SetArgs(fixture.planArgs("", "json"))
	if err := previewCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(preview.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	planPath, statePath, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := writePlanFile(planPath, plan); err != nil {
		t.Fatal(err)
	}
	saveSuccessfulTargetState(t, statePath, migrationStateIdentity(plan), "instance-1", 101, 201)

	var resumeOutput bytes.Buffer
	resume := newWodby1AppCommand()
	resume.SilenceUsage = true
	resume.SetOut(&resumeOutput)
	resume.SetErr(&resumeOutput)
	resume.SetArgs(append(fixture.planArgs("", "text"), "--apply"))
	err = resume.Execute()
	if err == nil || !strings.Contains(err.Error(), "saved target app ID 101 no longer exists") ||
		!strings.Contains(err.Error(), "--apply --restart") {
		t.Fatalf("resume error = %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("stale state was changed before explicit restart: %v", err)
	}
	resumeText := resumeOutput.String()
	planNotice := strings.Index(resumeText, "Step 1/4: Load saved migration")
	continueNotice := strings.Index(resumeText, "Step 2/4: Select run mode")
	targetValidation := strings.Index(resumeText, "Step 4/4: Validate saved target")
	missingError := strings.Index(resumeText, "saved target app ID 101 no longer exists")
	if planNotice < 0 || continueNotice < planNotice || targetValidation < continueNotice || missingError < targetValidation {
		t.Fatalf("stale-target output order is unclear:\n%s", resumeText)
	}
	if strings.Contains(resumeText, planPath) || strings.Contains(resumeText, statePath) {
		t.Fatalf("stale-target output exposed artifact paths without --verbose:\n%s", resumeText)
	}

	var output bytes.Buffer
	restart := newWodby1AppCommand()
	restart.SilenceUsage = true
	restart.SetOut(&output)
	restart.SetArgs(append(fixture.planArgs("", "text"), "--apply", "--restart"))
	err = restart.Execute()
	if err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("restart error = %v", err)
	}
	if !strings.Contains(output.String(), "Saved target app ID 101 no longer exists; --restart will replace its stale migration state.") {
		t.Fatalf("restart output is unclear:\n%s", output.String())
	}
	state, err := wodby1.InspectMigrationState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.App.TargetID == 101 {
		t.Fatal("restart preserved the deleted target app ID")
	}
}

func TestWodby1AppCommandFreshApplyReplacesStalePlan(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	planPath, statePath, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte("stale plan\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(append(fixture.planArgs("", "text"), "--apply"))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("apply error = %v", err)
	}

	plan, err := wodby1.LoadReviewedPlan(planPath)
	if err != nil {
		t.Fatalf("load replaced plan: %v", err)
	}
	if plan.Source.Kind != "app" || plan.Source.ID != "app-1" {
		t.Fatalf("replaced plan source = %#v", plan.Source)
	}
}

func TestWodby1AppCommandRestartReplansAfterDefinitiveRejection(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())

	var preview bytes.Buffer
	previewCmd := newWodby1AppCommand()
	previewCmd.SilenceUsage = true
	previewCmd.SetOut(&preview)
	previewCmd.SetArgs(fixture.planArgs("", "json"))
	if err := previewCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var oldPlan wodby1.Plan
	if err := json.Unmarshal(preview.Bytes(), &oldPlan); err != nil {
		t.Fatal(err)
	}
	planPath, statePath, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := writePlanFile(planPath, oldPlan); err != nil {
		t.Fatal(err)
	}
	saveDefinitivelyRejectedState(t, statePath, migrationStateIdentity(oldPlan), []string{"instance-1"})
	fixture.setSourceTitle("Changed after rejection")

	continueCmd := newWodby1AppCommand()
	continueCmd.SilenceUsage = true
	continueCmd.SetOut(&bytes.Buffer{})
	continueCmd.SetArgs(append(fixture.planArgs("", "text"), "--apply"))
	continueErr := continueCmd.Execute()
	if continueErr == nil || !strings.Contains(continueErr.Error(), "cannot continue from saved plan") ||
		!strings.Contains(continueErr.Error(), "saved plan was not overwritten") {
		t.Fatalf("continue error = %v", continueErr)
	}
	preservedPlan, err := wodby1.LoadReviewedPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if preservedPlan.PlanHash != oldPlan.PlanHash {
		t.Fatal("default continuation overwrote the saved plan after drift")
	}

	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(append(fixture.planArgs("", "text"), "--apply", "--restart"))
	applyErr := cmd.Execute()
	if applyErr == nil || !strings.Contains(applyErr.Error(), "app creation is ambiguous") ||
		strings.Contains(applyErr.Error(), "no longer match the applied plan") {
		t.Fatalf("apply error = %v", applyErr)
	}
	newPlan, err := wodby1.LoadReviewedPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if newPlan.PlanHash == oldPlan.PlanHash {
		t.Fatal("definitively rejected migration kept the stale plan")
	}
}

func TestWodby1AppCommandResumeStateRequiresSavedPlan(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	planPath, statePath, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(append(fixture.planArgs("", "text"), "--apply"))
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "migration state exists") ||
		!strings.Contains(err.Error(), "plan is missing") {
		t.Fatalf("apply error = %v", err)
	}
	if got := fixture.sourceRequestPaths(); len(got) != 0 {
		t.Fatalf("source requests = %#v", got)
	}
}

func TestWodby1AppCommandApplyResumePinsSavedStackRevision(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	t.Setenv("TMPDIR", t.TempDir())
	args := append(fixture.planArgs("", "text"), "--apply")

	first := newWodby1AppCommand()
	first.SilenceUsage = true
	first.SetOut(&bytes.Buffer{})
	first.SetArgs(args)
	if err := first.Execute(); err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("first apply error = %v", err)
	}
	planPath, _, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	fixture.setLatestStackRevision(72, 5)
	second := newWodby1AppCommand()
	second.SilenceUsage = true
	second.SetOut(&bytes.Buffer{})
	second.SetArgs(args)
	if err := second.Execute(); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resumed apply error = %v", err)
	}
	requests := fixture.targetRequestPaths()
	if !containsMigrationRequest(requests, "GET /v1/stack-revisions/71/services") {
		t.Fatalf("resume did not inspect saved stack revision: %#v", requests)
	}
	if containsMigrationRequest(requests, "GET /v1/stack-revisions/72/services") {
		t.Fatalf("resume substituted the latest stack revision: %#v", requests)
	}
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("resumed apply changed the saved plan")
	}
}

func TestWodby1AppCommandVerifyRequiresAppliedStateBeforeNetwork(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	setMigrationTargetConfig(t, "https://target.example.test/v1", "target-key", "")
	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"app-1",
		"--source-base-url", "https://source.example.test",
		"--source-token", testSourceToken,
		"--target-cluster", "33",
		"--target-stack-id", "7",
		"--verify",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no applied migration state found") ||
		!strings.Contains(err.Error(), "--apply first") {
		t.Fatalf("verify error = %v", err)
	}
}

func TestWodby1AppCommandPlansOrganizationOwnedTarget(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	var jsonOutput bytes.Buffer
	jsonCmd := newWodby1AppCommand()
	jsonCmd.SilenceUsage = true
	jsonCmd.SetOut(&jsonOutput)
	jsonCmd.SetArgs(fixture.organizationPlanArgs("", "json"))
	if err := jsonCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(jsonOutput.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	cmd.SetArgs(fixture.organizationPlanArgs("", "text"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if plan.Target.OrgID != 11 || plan.Target.ProjectID != 0 || plan.Target.Project != "" ||
		plan.Target.ClusterID != 33 || !plan.Target.DiscoveryVerified {
		t.Fatalf("target plan = %#v", plan.Target)
	}
	if !bytes.Contains(output.Bytes(), []byte("Target organization")) ||
		bytes.Contains(output.Bytes(), []byte("Target project")) {
		t.Fatalf("text plan does not show organization-owned target clearly:\n%s", output.Bytes())
	}
	requests := fixture.targetRequestPaths()
	for _, unexpected := range []string{
		"GET /v1/projects/22",
		"GET /v1/clusters?orgId=11&projectIds=22",
	} {
		if containsMigrationRequest(requests, unexpected) {
			t.Fatalf("organization-owned discovery made project-scoped request %q: %#v", unexpected, requests)
		}
	}
	if !containsMigrationRequest(requests, "GET /v1/stacks/7") {
		t.Fatalf("target requests = %#v, missing explicit stack lookup", requests)
	}
}

func TestWodby1AppCommandRejectsInvalidLocalInputBeforeNetwork(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "mutually exclusive actions",
			args: []string{"--apply", "--verify", "--target-stack-id", "7"},
			want: "--apply and --verify cannot be used together",
		},
		{
			name: "restart requires apply",
			args: []string{"--restart", "--target-stack-id", "7"},
			want: "--restart requires --apply",
		},
		{
			name: "force requires data import",
			args: []string{"--force", "--skip-data", "--target-stack-id", "7"},
			want: "--force cannot be used with --skip-data",
		},
		{
			name: "mapping",
			args: []string{"--target-env-map", "not-a-mapping"},
			want: "source=target format",
		},
		{
			name: "conflicting mapping",
			args: []string{
				"--target-service-map", "php=php",
				"--target-service-map", "php=nginx",
			},
			want: "conflicting mappings",
		},
		{
			name: "target stack mapping must use ID",
			args: []string{"--target-stack-map", "drupal11=acme/drupal11"},
			want: "must be a positive stack ID",
		},
		{
			name: "source token",
			args: []string{"--source-token", "short"},
			want: "exactly 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.Error(w, "unexpected network request", http.StatusInternalServerError)
			}))
			defer server.Close()
			setMigrationTargetConfig(t, server.URL+"/v1", "target-key", "")

			args := []string{
				"app-1",
				"--source-base-url", server.URL,
				"--source-token", testSourceToken,
				"--target-project", "22",
				"--target-cluster", "33",
			}
			args = append(args, tt.args...)
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
			if requests != 0 {
				t.Fatalf("invalid local input made %d network request(s)", requests)
			}
		})
	}
}

func TestPrintPreviewExplainsBlockingNextStep(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	plan := wodby1.Plan{
		Status:  "blocked",
		Source:  wodby1.PlanSource{Kind: "instance"},
		Summary: wodby1.PlanSummary{Blocking: 2},
	}
	if err := printPreview(cmd, plan); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"Next step:",
		"Fix the 2 blocking item(s) above",
		"rerun this preview",
		"cannot start until the plan has no blockers",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("blocked plan output does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "--apply") {
		t.Fatalf("blocked plan suggests applying:\n%s", text)
	}
}

func TestPrintBlockedApplyReviewSaysMigrationDidNotStart(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	plan := wodby1.Plan{
		Status:  "blocked",
		Source:  wodby1.PlanSource{Kind: "instance"},
		Summary: wodby1.PlanSummary{Blocking: 1},
	}
	if err := printBlockedApplyReview(cmd, plan); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Migration not started") ||
		!strings.Contains(text, "Resolve the 1 blocking item") {
		t.Fatalf("blocked apply output is unclear:\n%s", text)
	}
	if strings.Contains(text, "Applying the migration plan") {
		t.Fatalf("blocked apply claims that migration is starting:\n%s", text)
	}
}

func TestArtifactPathsDefaultToSystemTemporaryDirectoryWithoutCreatingIt(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	planPath, statePath, err := artifactPaths("instance", "instance-1", "")
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(tempDir, "wodby-migrations")
	if filepath.Dir(planPath) != wantDir || filepath.Dir(statePath) != wantDir {
		t.Fatalf("artifact paths = %q, %q; want directory %q", planPath, statePath, wantDir)
	}
	if filepath.Base(planPath) != "wodby1-instance-instance-1.migration-plan.json" ||
		filepath.Base(statePath) != "wodby1-instance-instance-1.migration-state.json" {
		t.Fatalf("artifact paths = %q, %q", planPath, statePath)
	}
	if _, err := os.Stat(wantDir); !os.IsNotExist(err) {
		t.Fatalf("artifact path calculation created directory: %v", err)
	}
}

func TestArtifactPathsRejectPlanAndStateCollision(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	planPath, _, err := artifactPaths("app", "app-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := artifactPaths("app", "app-1", planPath); err == nil ||
		!strings.Contains(err.Error(), "temporary migration plan path") {
		t.Fatalf("artifactPaths() error = %v", err)
	}
}

func TestServerArtifactPathsRejectPlanAndChildStateCollision(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "server-state.json")
	planPath := serverAppStatePath(statePath, "app-1")
	err := validateServerArtifactPaths(planPath, statePath, wodby1.Export{
		Apps: []wodby1.AppExport{{App: wodby1.App{UUID: "app-1"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "per-app migration state path") {
		t.Fatalf("collision error = %v", err)
	}
}

type migrationAPIFixture struct {
	t             *testing.T
	role          string
	status        string
	platformAdmin bool

	source *httptest.Server
	target *httptest.Server

	mu                 sync.Mutex
	sourceRequests     []string
	targetRequests     []string
	sourceRequestToken string
	mutations          int
	sourceTitle        string
	sourceKind         string
	stackRevID         int
	stackRevNumber     int
}

func newMigrationAPIFixture(
	t *testing.T,
	role string,
	status string,
	platformAdmin bool,
) *migrationAPIFixture {
	t.Helper()
	fixture := &migrationAPIFixture{
		t:              t,
		role:           role,
		status:         status,
		platformAdmin:  platformAdmin,
		sourceTitle:    "Demo",
		sourceKind:     "app",
		stackRevID:     71,
		stackRevNumber: 4,
	}
	fixture.source = httptest.NewServer(http.HandlerFunc(fixture.handleSource))
	fixture.target = httptest.NewServer(http.HandlerFunc(fixture.handleTarget))
	return fixture
}

func (f *migrationAPIFixture) Close() {
	f.source.Close()
	f.target.Close()
}

func (f *migrationAPIFixture) planArgs(planPath string, output string) []string {
	_ = planPath
	return []string{
		"app-1",
		"--source-base-url", f.source.URL,
		"--source-token", testSourceToken,
		"--target-project", "22",
		"--target-cluster", "33",
		"--target-stack-id", "7",
		"--target-env-map", "prod=production",
		"--skip-code",
		"--skip-data",
		"--output", output,
	}
}

func (f *migrationAPIFixture) organizationPlanArgs(planPath string, output string) []string {
	_ = planPath
	return []string{
		"app-1",
		"--source-base-url", f.source.URL,
		"--source-token", testSourceToken,
		"--target-cluster", "33",
		"--target-stack-id", "7",
		"--target-env-map", "prod=production",
		"--skip-code",
		"--skip-data",
		"--output", output,
	}
}

func (f *migrationAPIFixture) instancePlanArgs(planPath string, output string) []string {
	args := f.planArgs(planPath, output)
	args[0] = "instance-1"
	return args
}

func (f *migrationAPIFixture) serverPlanArgs(planPath string, output string) []string {
	_ = planPath
	return []string{
		"server-1",
		"--source-base-url", f.source.URL,
		"--source-token", testSourceToken,
		"--target-project", "22",
		"--target-cluster", "33",
		"--target-stack-id", "7",
		"--target-env-map", "prod=production",
		"--skip-code",
		"--skip-data",
		"--output", output,
	}
}

func (f *migrationAPIFixture) handleSource(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.sourceRequests = append(f.sourceRequests, r.Method+" "+r.URL.RequestURI())
	f.sourceRequestToken = r.Header.Get("X-API-Key")
	title := f.sourceTitle
	kind := f.sourceKind
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if kind == "server" {
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema:          wodby1.ExportSchemaV2,
			GeneratedAt:     1234,
			Source:          &wodby1.ExportSource{Kind: "server", UUID: "server-1"},
			SecretsIncluded: true,
			Apps: []wodby1.AppExport{
				{
					App: wodby1.App{UUID: "app-1", Name: "demo", Title: title, Type: "app", Status: "ok"},
					Instances: []wodby1.Instance{{
						UUID: "instance-1", Name: "prod", Title: "Production", Type: "prod", Status: "ok",
						Stack: wodby1.Stack{Name: "drupal11", Version: "11"},
					}},
				},
				{
					App: wodby1.App{UUID: "app-2", Name: "second", Title: "Second", Type: "app", Status: "ok"},
					Instances: []wodby1.Instance{{
						UUID: "instance-2", Name: "prod", Title: "Production", Type: "prod", Status: "ok",
						Stack: wodby1.Stack{Name: "drupal11", Version: "11"},
					}},
				},
			},
		})
		return
	}
	if kind == "instance" {
		_ = json.NewEncoder(w).Encode(wodby1.Export{
			Schema:          wodby1.ExportSchemaV2,
			GeneratedAt:     1234,
			Source:          &wodby1.ExportSource{Kind: "instance", UUID: "instance-1"},
			SecretsIncluded: true,
			Apps: []wodby1.AppExport{{
				App: wodby1.App{
					UUID: "app-1", Name: "demo", Title: title, Type: "app", Status: "ok",
				},
				Instances: []wodby1.Instance{{
					UUID: "instance-1", Name: "prod", Title: "Production", Type: "prod", Status: "ok",
					Stack: wodby1.Stack{Name: "drupal11", Version: "11"},
				}},
			}},
		})
		return
	}
	_ = json.NewEncoder(w).Encode(wodby1.Export{
		Schema:          wodby1.ExportSchemaV2,
		GeneratedAt:     1234,
		Source:          &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
		SecretsIncluded: true,
		Apps: []wodby1.AppExport{{
			App: wodby1.App{
				UUID: "app-1", Name: "demo", Title: title, Type: "app", Status: "ok",
			},
			Instances: []wodby1.Instance{{
				UUID:   "instance-1",
				Name:   "prod",
				Title:  "Production",
				Type:   "prod",
				Status: "ok",
				Stack:  wodby1.Stack{Name: "drupal11", Version: "11"},
			}},
		}},
	})
}

func (f *migrationAPIFixture) handleTarget(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.targetRequests = append(f.targetRequests, r.Method+" "+r.URL.RequestURI())
	if r.Method != http.MethodGet {
		f.mutations++
	}
	stackRevID := f.stackRevID
	stackRevNumber := f.stackRevNumber
	f.mu.Unlock()

	if r.Method != http.MethodGet {
		http.Error(w, "unexpected target mutation", http.StatusInternalServerError)
		return
	}
	userID := 7
	cluster := wodby1.TargetCluster{
		ID: 33, Name: "primary", Title: "Primary", Status: "OK", OrgID: 11,
		OwnershipScope: wodby1.TargetOwnershipScopeOrg,
		Capabilities: wodby1.TargetClusterCapabilities{
			EnvoyGateway: true, RedirectRoutes: true,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/apps":
		writeMigrationJSON(w, []wodby1.TargetApp{})
	case "/v1/orgs":
		writeMigrationJSON(w, []wodby1.TargetOrg{migrationTargetOrgFixture()})
	case "/v1/orgs/11":
		writeMigrationJSON(w, migrationTargetOrgFixture())
	case "/v1/user":
		writeMigrationJSON(w, wodby1.TargetCurrentUser{
			ID: userID, Email: "customer@example.test", IsAdmin: f.platformAdmin,
		})
	case "/v1/org-memberships":
		writeMigrationJSON(w, []wodby1.TargetOrgMembership{{
			ID: 51, UserID: &userID, OrgID: 11, Role: f.role, Status: f.status,
		}})
	case "/v1/projects/22":
		writeMigrationJSON(w, wodby1.TargetProject{ID: 22, Name: "website", Title: "Website", OrgID: 11})
	case "/v1/clusters/33":
		writeMigrationJSON(w, cluster)
	case "/v1/clusters":
		writeMigrationJSON(w, []wodby1.TargetCluster{cluster})
	case "/v1/envs":
		writeMigrationJSON(w, []wodby1.TargetEnv{{
			ID: 44, Name: "production", Title: "Production", Type: "PROD", OrgID: 11,
		}})
	case "/v1/stacks":
		if r.URL.Query().Get("orgId") != "11" ||
			(r.URL.Query().Get("projectIds") != "" && r.URL.Query().Get("projectIds") != "22") ||
			r.URL.Query().Get("search") != "drupal11" {
			http.Error(w, "invalid stack query", http.StatusBadRequest)
			return
		}
		originName := "drupal11"
		writeMigrationJSON(w, wodby1.TargetStacksResponse{
			Items: []wodby1.TargetStack{{
				ID: 7, Name: "acme/drupal11", Title: "Drupal 11", Status: "OK",
				RevID: stackRevID, LatestRevNumber: stackRevNumber,
				OriginStackRevName: &originName, OrgID: 11,
			}},
			TotalCount: 1,
		})
	case "/v1/stacks/7":
		originName := "drupal11"
		writeMigrationJSON(w, wodby1.TargetStack{
			ID: 7, Name: "acme/drupal11", Title: "Drupal 11", Status: "OK",
			RevID: stackRevID, LatestRevNumber: stackRevNumber,
			OriginStackRevName: &originName, OrgID: 11,
		})
	case "/v1/stack-revisions/71":
		writeMigrationJSON(w, wodby1.TargetStackRevision{
			ID: 71, Name: "drupal11", Number: 4, Version: "11", StackID: 7,
		})
	case "/v1/stack-revisions/72":
		writeMigrationJSON(w, wodby1.TargetStackRevision{
			ID: 72, Name: "drupal11", Number: 5, Version: "11", StackID: 7,
		})
	case "/v1/stack-revisions/71/services":
		writeMigrationJSON(w, []wodby1.TargetStackService{})
	default:
		http.NotFound(w, r)
	}
}

func migrationTargetOrgFixture() wodby1.TargetOrg {
	return wodby1.TargetOrg{
		ID: 11, Name: "acme", Title: "Acme",
		Capabilities: &wodby1.TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
		Subscription: &wodby1.TargetOrgSubscription{
			Status: "ACTIVE",
			Plan: &wodby1.TargetOrgSubscriptionPlan{
				Name: "team", Title: "Team", Usage: 2, UsageIncluded: 10,
			},
		},
	}
}

func (f *migrationAPIFixture) sourceRequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sourceRequests)
}

func (f *migrationAPIFixture) sourceRequestPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sourceRequests...)
}

func (f *migrationAPIFixture) sourceAPIKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sourceRequestToken
}

func (f *migrationAPIFixture) targetRequestPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.targetRequests...)
}

func (f *migrationAPIFixture) mutationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mutations
}

func (f *migrationAPIFixture) setLatestStackRevision(id int, number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stackRevID = id
	f.stackRevNumber = number
}

func (f *migrationAPIFixture) setSourceTitle(title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sourceTitle = title
}

func (f *migrationAPIFixture) setSourceKind(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sourceKind = kind
}

func setMigrationTargetConfig(t *testing.T, endpoint string, apiKey string, accessToken string) {
	t.Helper()
	previousEndpoint := viper.GetString("api_base_url")
	previousAPIKey := viper.GetString("api_key")
	previousAccessToken := viper.GetString("access_token")
	viper.Set("api_base_url", endpoint)
	viper.Set("api_key", apiKey)
	viper.Set("access_token", accessToken)
	t.Cleanup(func() {
		viper.Set("api_base_url", previousEndpoint)
		viper.Set("api_key", previousAPIKey)
		viper.Set("access_token", previousAccessToken)
	})
}

func writeMigrationJSON(w http.ResponseWriter, value interface{}) {
	_ = json.NewEncoder(w).Encode(value)
}

func containsMigrationRequest(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func saveDefinitivelyRejectedState(
	t *testing.T,
	path string,
	identity wodby1.MigrationStateIdentity,
	instanceIDs []string,
) {
	t.Helper()
	state, err := wodby1.NewMigrationState(identity, instanceIDs)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetPhase(wodby1.MigrationPhasePrepare); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationFailure("create", "api_rejected"); err != nil {
		t.Fatal(err)
	}
	if err := state.SetAppTarget(0, wodby1.MigrationResourceFailed); err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range instanceIDs {
		if err := state.MarkInstanceOperationIntent(sourceID, "create"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkInstanceOperationFailure(sourceID, "create", "api_rejected"); err != nil {
			t.Fatal(err)
		}
		if err := state.SetInstanceTarget(sourceID, 0, wodby1.MigrationResourceFailed); err != nil {
			t.Fatal(err)
		}
	}
	if err := wodby1.SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
}

func saveSuccessfulTargetState(
	t *testing.T,
	path string,
	identity wodby1.MigrationStateIdentity,
	sourceInstanceID string,
	targetAppID int,
	targetInstanceID int,
) {
	t.Helper()
	state, err := wodby1.NewMigrationState(identity, []string{sourceInstanceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetPhase(wodby1.MigrationPhasePrepare); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs("create", targetAppID, 0); err != nil {
		t.Fatal(err)
	}
	if err := state.SetAppTarget(targetAppID, wodby1.MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationIntent(sourceInstanceID, "create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationSuccessWithIDs(sourceInstanceID, "create", targetInstanceID, 0); err != nil {
		t.Fatal(err)
	}
	if err := state.SetInstanceTarget(sourceInstanceID, targetInstanceID, wodby1.MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.SetPhase(wodby1.MigrationPhaseSyncData); err != nil {
		t.Fatal(err)
	}
	if err := wodby1.SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
}

func removeMigrationFlag(args []string, flag string) []string {
	result := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] == flag {
			index++
			continue
		}
		result = append(result, args[index])
	}
	return result
}
