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

func TestWodby1ServerCommandPlansEverySourceApp(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "owner", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("server")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	planPath := filepath.Join(t.TempDir(), "server-plan.json")
	cmd := newWodby1ServerCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(fixture.serverPlanArgs(planPath, "json"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	plan := readMigrationPlan(t, planPath)
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
	planPath := filepath.Join(dir, "server-plan.json")
	statePath := filepath.Join(dir, "server-state.json")

	planCmd := newWodby1ServerCommand()
	planCmd.SilenceUsage = true
	planCmd.SetOut(&bytes.Buffer{})
	planCmd.SetArgs(fixture.serverPlanArgs(planPath, "text"))
	if err := planCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	plan := readMigrationPlan(t, planPath)

	args := fixture.serverPlanArgs(planPath, "text")
	args = append(args,
		"--state-file", statePath,
		"--phase", "prepare",
		"--approve-plan", plan.PlanHash,
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

func TestWodby1AppCommandRequiresTargetSelectorsAndCredentials(t *testing.T) {
	tests := []struct {
		name       string
		targetOrg  string
		project    string
		cluster    string
		apiBaseURL string
		apiKey     string
		access     string
		want       string
	}{
		{
			name: "organization", project: "22", cluster: "33",
			apiBaseURL: "https://target.example.test/v1", apiKey: "key",
			want: "--target-org, --target-project, and --target-cluster are required",
		},
		{
			name: "project", targetOrg: "11", cluster: "33",
			apiBaseURL: "https://target.example.test/v1", apiKey: "key",
			want: "--target-org, --target-project, and --target-cluster are required",
		},
		{
			name: "cluster", targetOrg: "11", project: "22",
			apiBaseURL: "https://target.example.test/v1", apiKey: "key",
			want: "--target-org, --target-project, and --target-cluster are required",
		},
		{
			name: "target API base URL", targetOrg: "11", project: "22", cluster: "33",
			apiKey: "key",
			want:   "--api-base-url is required",
		},
		{
			name: "target API credentials", targetOrg: "11", project: "22", cluster: "33",
			apiBaseURL: "https://target.example.test/v1",
			want:       "--api-key is required",
		},
		{
			name: "target access token is not sufficient", targetOrg: "11", project: "22", cluster: "33",
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
				"--target-org", tt.targetOrg,
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

			planPath := filepath.Join(t.TempDir(), "plan.json")
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetArgs(fixture.planArgs(planPath, "json"))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}

			plan := readMigrationPlan(t, planPath)
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
			cmd.SetArgs(fixture.planArgs(filepath.Join(t.TempDir(), "plan.json"), "text"))
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
	args := fixture.planArgs(filepath.Join(t.TempDir(), "plan.json"), "json")
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

func TestWodby1AppCommandPreflightsAndWritesSecurePlan(t *testing.T) {
	tests := []struct {
		output string
		check  func(*testing.T, []byte)
	}{
		{
			output: "text",
			check: func(t *testing.T, output []byte) {
				t.Helper()
				if !bytes.Contains(output, []byte("Wodby 1 to Wodby 2 migration plan")) ||
					!bytes.Contains(output, []byte("Plan file:")) {
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
				if plan.Schema != "wodby1-migration-plan/v3" || len(plan.PlanHash) != 64 {
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

			planPath := filepath.Join(t.TempDir(), "migration-plan.json")
			var output bytes.Buffer
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetOut(&output)
			cmd.SetArgs(fixture.planArgs(planPath, tt.output))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			tt.check(t, output.Bytes())

			info, err := os.Stat(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0600 {
				t.Fatalf("plan permissions = %o, want 600", got)
			}
			plan := readMigrationPlan(t, planPath)
			if plan.Target.OrgID != 11 ||
				plan.Target.ProjectID != 22 ||
				plan.Target.ClusterID != 33 ||
				!plan.Target.DiscoveryVerified ||
				!plan.Target.OrgOwnerOrAdminVerified {
				t.Fatalf("target plan = %#v", plan.Target)
			}
			instance := plan.Apps[0].Instances[0]
			if instance.TargetEnvID != 44 ||
				instance.Stack.Target != "acme/drupal11" ||
				instance.Stack.TargetID != 7 ||
				instance.Stack.TargetRevID != 71 {
				t.Fatalf("preflighted instance = %#v", instance)
			}

			requests := fixture.targetRequestPaths()
			for _, want := range []string{
				"GET /v1/orgs/11",
				"GET /v1/user",
				"GET /v1/org-memberships?orgId=11",
				"GET /v1/projects/22",
				"GET /v1/clusters/33",
				"GET /v1/clusters?orgId=11&projectIds=22",
				"GET /v1/envs?orgId=11",
				"GET /v1/stacks?orgId=11&page=1&pageSize=100&projectIds=22&search=drupal11",
				"GET /v1/stack-revisions/71/services",
			} {
				if !containsMigrationRequest(requests, want) {
					t.Fatalf("target requests = %#v, missing %q", requests, want)
				}
			}
		})
	}
}

func TestWodby1AppCommandRejectsInvalidLocalInputBeforeNetwork(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "phase",
			args: []string{"--phase", "copy-everything"},
			want: "unsupported --phase",
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
				"--target-org", "11",
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

func TestWodby1MutationPhaseRequiresExactPlanApprovalBeforeWrites(t *testing.T) {
	for _, approval := range []string{"", strings.Repeat("0", 64)} {
		name := "missing"
		if approval != "" {
			name = "wrong"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, "admin", "ok", false)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

			planPath := filepath.Join(t.TempDir(), "plan.json")
			planCmd := newWodby1AppCommand()
			planCmd.SilenceUsage = true
			planCmd.SetOut(&bytes.Buffer{})
			planCmd.SetArgs(fixture.planArgs(planPath, "text"))
			if err := planCmd.Execute(); err != nil {
				t.Fatal(err)
			}
			plan := readMigrationPlan(t, planPath)
			planBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}

			args := fixture.planArgs(planPath, "text")
			args = append(args, "--phase", "prepare")
			if approval != "" {
				args = append(args, "--approve-plan", approval)
			}
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetArgs(args)
			err = cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "requires --approve-plan") {
				t.Fatalf("error = %v", err)
			}

			if !strings.Contains(err.Error(), plan.PlanHash) {
				t.Fatalf("error %q does not require exact plan hash %q", err, plan.PlanHash)
			}
			if got := fixture.mutationCount(); got != 0 {
				t.Fatalf("unapproved phase issued %d target mutation(s)", got)
			}
			after, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, planBytes) {
				t.Fatal("unapproved mutation phase overwrote the reviewed plan")
			}
		})
	}
}

func TestWodby1MutationPhaseRejectsMissingInsecureAndInvalidReviewedPlanBeforeNetwork(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *migrationAPIFixture, string) string
		want    string
	}{
		{
			name: "missing",
			prepare: func(_ *testing.T, _ *migrationAPIFixture, _ string) string {
				return strings.Repeat("0", 64)
			},
			want: "load reviewed migration plan",
		},
		{
			name: "insecure",
			prepare: func(t *testing.T, fixture *migrationAPIFixture, path string) string {
				t.Helper()
				plan := runMigrationPlan(t, fixture, path)
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
				return plan.PlanHash
			},
			want: "permissions are not 0600",
		},
		{
			name: "invalid hash",
			prepare: func(t *testing.T, fixture *migrationAPIFixture, path string) string {
				t.Helper()
				plan := runMigrationPlan(t, fixture, path)
				plan.Apps[0].Title = "tampered"
				writeMigrationPlan(t, path, plan)
				return plan.PlanHash
			},
			want: "plan hash does not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, "admin", "ok", false)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "ignored-access-token")
			planPath := filepath.Join(t.TempDir(), "plan.json")
			approval := test.prepare(t, fixture, planPath)
			sourceBefore := fixture.sourceRequestCount()
			targetBefore := len(fixture.targetRequestPaths())

			args := fixture.planArgs(planPath, "text")
			args = append(args, "--phase", "prepare", "--approve-plan", approval)
			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
			if fixture.sourceRequestCount() != sourceBefore ||
				len(fixture.targetRequestPaths()) != targetBefore {
				t.Fatal("invalid reviewed plan triggered an API request")
			}
			if fixture.mutationCount() != 0 {
				t.Fatal("invalid reviewed plan triggered a target mutation")
			}
		})
	}
}

func TestWodby1MutationPhasePinsReviewedStackRevisionAndDoesNotRewritePlan(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
	planPath := filepath.Join(t.TempDir(), "plan.json")
	plan := runMigrationPlan(t, fixture, planPath)
	reviewedBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.setLatestStackRevision(72, 5)

	args := fixture.planArgs(planPath, "text")
	args = append(args, "--phase", "prepare", "--approve-plan", plan.PlanHash)
	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(args)
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("prepare error = %v", err)
	}
	if fixture.mutationCount() != 1 {
		t.Fatalf("target mutations = %d, want authorized create attempt", fixture.mutationCount())
	}
	requests := fixture.targetRequestPaths()
	for _, want := range []string{
		"GET /v1/stacks/7",
		"GET /v1/stack-revisions/71",
		"GET /v1/stack-revisions/71/services",
	} {
		if !containsMigrationRequest(requests, want) {
			t.Fatalf("target requests = %#v, missing pinned read %q", requests, want)
		}
	}
	if containsMigrationRequest(requests, "GET /v1/stack-revisions/72/services") {
		t.Fatalf("prepare substituted latest stack revision: %#v", requests)
	}
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, reviewedBytes) {
		t.Fatal("mutation phase rewrote the reviewed plan")
	}
}

func TestWodby1MutationPhaseRejectsSourceAndOptionDriftWithoutRewritingPlan(t *testing.T) {
	tests := []struct {
		name   string
		change func(*migrationAPIFixture, *[]string)
	}{
		{
			name: "source changed",
			change: func(fixture *migrationAPIFixture, _ *[]string) {
				fixture.setSourceTitle("Changed title")
			},
		},
		{
			name: "options changed",
			change: func(_ *migrationAPIFixture, args *[]string) {
				*args = append(*args, "--target-stack-map", "drupal11=acme/drupal11")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMigrationAPIFixture(t, "admin", "ok", false)
			defer fixture.Close()
			setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")
			planPath := filepath.Join(t.TempDir(), "plan.json")
			plan := runMigrationPlan(t, fixture, planPath)
			reviewedBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			args := fixture.planArgs(planPath, "text")
			args = append(args, "--phase", "prepare", "--approve-plan", plan.PlanHash)
			test.change(fixture, &args)

			cmd := newWodby1AppCommand()
			cmd.SilenceUsage = true
			cmd.SetArgs(args)
			err = cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "no longer match") {
				t.Fatalf("error = %v", err)
			}
			if fixture.mutationCount() != 0 {
				t.Fatalf("drift triggered %d target mutation(s)", fixture.mutationCount())
			}
			after, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, reviewedBytes) {
				t.Fatal("drift rejection rewrote the reviewed plan")
			}
		})
	}
}

func TestArtifactPathsRejectSameCanonicalPlanAndStatePath(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "artifacts")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(dir, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(realDir, "migration.json")
	stateAlias := filepath.Join(aliasDir, "migration.json")
	if _, _, err := artifactPaths("app-1", planPath, stateAlias); err == nil ||
		!strings.Contains(err.Error(), "different paths") {
		t.Fatalf("artifactPaths() error = %v", err)
	}

	existing := filepath.Join(realDir, "existing.json")
	hardlink := filepath.Join(realDir, "hardlink.json")
	if err := os.WriteFile(existing, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(existing, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := artifactPaths("app-1", existing, hardlink); err == nil ||
		!strings.Contains(err.Error(), "different paths") {
		t.Fatalf("hard-linked artifactPaths() error = %v", err)
	}

	lockState := filepath.Join(realDir, "state.json")
	if _, _, err := artifactPaths("app-1", lockState+".lock", lockState); err == nil ||
		!strings.Contains(err.Error(), "state lock path") {
		t.Fatalf("state-lock artifactPaths() error = %v", err)
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
	return []string{
		"app-1",
		"--source-base-url", f.source.URL,
		"--source-token", testSourceToken,
		"--target-org", "11",
		"--target-project", "22",
		"--target-cluster", "33",
		"--target-env-map", "prod=production",
		"--skip-code",
		"--skip-data",
		"--plan-file", planPath,
		"--output", output,
	}
}

func (f *migrationAPIFixture) serverPlanArgs(planPath string, output string) []string {
	return []string{
		"server-1",
		"--source-base-url", f.source.URL,
		"--source-token", testSourceToken,
		"--target-org", "11",
		"--target-project", "22",
		"--target-cluster", "33",
		"--target-env-map", "prod=production",
		"--skip-code",
		"--skip-data",
		"--plan-file", planPath,
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
		Capabilities: wodby1.TargetClusterCapabilities{
			EnvoyGateway: true, RedirectRoutes: true,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/apps":
		writeMigrationJSON(w, []wodby1.TargetApp{})
	case "/v1/orgs/11":
		writeMigrationJSON(w, wodby1.TargetOrg{ID: 11, Name: "acme", Title: "Acme"})
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
			r.URL.Query().Get("projectIds") != "22" ||
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

func readMigrationPlan(t *testing.T, path string) wodby1.Plan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan wodby1.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func runMigrationPlan(
	t *testing.T,
	fixture *migrationAPIFixture,
	path string,
) wodby1.Plan {
	t.Helper()
	cmd := newWodby1AppCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs(fixture.planArgs(path, "text"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return readMigrationPlan(t, path)
}

func writeMigrationPlan(t *testing.T, path string, plan wodby1.Plan) {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
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
