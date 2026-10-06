package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wodby/wodby-cli/pkg/types"
)

// workspaceCatalog is a stack with a code service, a web server that mounts
// its code, an SSH derivative and a database.
func workspaceCatalog(eligibility *TargetWorkspaceEligibility) preflightTargetCatalog {
	return preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8},
		},
		stackServices: map[int][]TargetStackService{71: {
			{ID: 11, Name: "php", ServiceRevID: 101},
			{ID: 12, Name: "nginx", ServiceRevID: 102},
			{ID: 13, Name: "sshd", Type: "ssh", ServiceRevID: 101},
			{ID: 14, Name: "mariadb", Disabled: true, ServiceRevID: 103},
		}},
		revisions: map[int]TargetServiceRevision{
			101: {ID: 101, Name: "drupal11-php", Type: "service", ServiceID: 201, Manifest: &TargetServiceManifest{
				Name: "drupal11-php", Build: &TargetServiceBuildCapability{Connect: true},
			}},
			102: {ID: 102, Name: "nginx", ServiceID: 202, Manifest: &TargetServiceManifest{Name: "nginx"}},
			103: {ID: 103, Name: "mariadb", ServiceID: 203, Manifest: &TargetServiceManifest{Name: "mariadb"}},
		},
		workspaceEligibility: eligibility,
	}
}

func eligibleWorkspace() *TargetWorkspaceEligibility {
	source := 11
	return &TargetWorkspaceEligibility{Eligible: true, SourceStackServiceID: &source, ConsumerStackServiceIDs: []int{12}}
}

func workspaceBlockers(plan Plan) []string {
	messages := []string{}
	for _, item := range plan.Review {
		if item.Severity == SeverityBlocking {
			messages = append(messages, item.Subject+": "+item.Message)
		}
	}
	return messages
}

func TestPreflightWorkspaceInstance(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true}, {Name: "nginx", Enabled: true},
	}
	api := newPreflightTargetAPI(t, workspaceCatalog(eligibleWorkspace()))
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
	options.WorkspaceInstances = map[string]bool{"inst-1": true}
	plan := preflightBuildPlan(t, export, options)
	opts := TargetPreflightOptions{SkipData: true}
	prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	if blockers := workspaceBlockers(plan); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	instance := prepared.Instances[0]
	workspace := instance.Workspace
	if workspace == nil || workspace.Branch != "main" || workspace.SourceServiceName != "php" ||
		!reflect.DeepEqual(workspace.CodeServiceNames, map[string]bool{"php": true, "nginx": true}) {
		t.Fatalf("workspace = %#v", workspace)
	}
	if planned := plan.Apps[0].Instances[0].Workspace; planned == nil || planned.Branch != "main" {
		t.Fatalf("planned workspace = %#v", planned)
	}
	// A workspace is not built: no CI of any kind, and no pipeline check.
	if instance.UsesWodbyCI || instance.ExternalCIOnly || instance.CIIntegrationKey != "" || len(prepared.Integrations) != 0 {
		t.Fatalf("workspace must not use CI: %#v", instance)
	}
	for _, path := range api.paths {
		if strings.HasSuffix(path, "/options/remote-git-repo-file") {
			t.Fatalf("workspace checked for a CI pipeline: %s", path)
		}
	}
	if instance.EffectiveState["sshd"] {
		t.Fatal("the stack's SSH service must stay disabled in a workspace")
	}
	// The check describes the services exactly as they will be created.
	cluster := 10
	want := TargetWorkspaceEligibilityInput{StackRevID: 71, ClusterID: &cluster, DisabledServiceIDs: []int{13, 14}}
	if len(api.workspaceChecks) != 1 || !reflect.DeepEqual(api.workspaceChecks[0], want) {
		t.Fatalf("eligibility requests = %#v, want %#v", api.workspaceChecks, want)
	}
	confirmed := false
	for _, item := range plan.Review {
		confirmed = confirmed || (item.Severity == SeverityConfirmation && item.Subject == "development workspace")
	}
	if !confirmed {
		t.Fatal("the plan must ask to confirm the workspace")
	}
	// Repeating preflight must accept the reviewed selection without drift.
	if _, err := api.client.PreflightTarget(context.Background(), export, &plan, opts); err != nil {
		t.Fatal(err)
	}

	mode, input, overrides, err := workspaceCreationInput(instance)
	if err != nil {
		t.Fatal(err)
	}
	if mode != TargetExecutionModeWorkspace || input == nil || input.Branch != "main" {
		t.Fatalf("creation mode = %q, workspace = %#v", mode, input)
	}
	if len(overrides) != 2 || overrides[0].ID != 11 || overrides[1].ID != 13 {
		t.Fatalf("overrides = %#v", overrides)
	}
	source := overrides[0].BuildSource
	if source == nil || source.BuildSourceType != TargetBuildSourceConnect || *source.IntegrationID != 44 ||
		*source.RemoteGitRepoID != "remote-repo-17" || *source.GitRef != "main" || *source.GitRefType != TargetGitRefBranch {
		t.Fatalf("workspace repository = %#v", source)
	}
	if overrides[0].Disabled != nil || overrides[1].Disabled == nil || !*overrides[1].Disabled {
		t.Fatalf("only services that differ from the stack default are overridden: %#v", overrides)
	}
	if err := validateTargetWorkspaceCreation(mode, input, overrides); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightWorkspaceBlockers(t *testing.T) {
	otherSource := 12
	for _, tc := range []struct {
		name        string
		eligibility *TargetWorkspaceEligibility
		properties  map[string]interface{}
		repository  bool
		integration int
		skipCode    bool
		want        string
	}{
		{name: "services not supported", repository: true, integration: 44,
			eligibility: &TargetWorkspaceEligibility{Reasons: []string{"php does not support development workspaces"}},
			want:        "php does not support development workspaces"},
		{name: "installation without workspaces", repository: true, integration: 44,
			want: "does not support development workspaces"},
		{name: "code held by another service", repository: true, integration: 44,
			eligibility: &TargetWorkspaceEligibility{Eligible: true, SourceStackServiceID: &otherSource},
			want:        "--target-code-service nginx"},
		{name: "tag deployment", repository: true, integration: 44, eligibility: eligibleWorkspace(),
			properties: map[string]interface{}{"deployment_type": "git", "git_target_value": "v1.2", "git_target_type": "tag"},
			want:       "created from a branch"},
		{name: "repository not linked", repository: true, eligibility: eligibleWorkspace(),
			want: "--target-git-integration-id"},
		{name: "no source repository", eligibility: eligibleWorkspace(),
			want: "no connected Git repository"},
		{name: "third-party CI instance", repository: true, integration: 44, eligibility: eligibleWorkspace(),
			properties: map[string]interface{}{"deployment_type": "ci", "git_target_value": "main", "git_target_type": "branch"},
			want:       "direct Git deployment"},
		{name: "code skipped", repository: true, integration: 44, eligibility: eligibleWorkspace(), skipCode: true,
			want: "remove --skip-code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			export := preflightFixtureExport(tc.repository)
			source := &export.Apps[0].Instances[0]
			source.Services = []Service{{Name: "php", Enabled: true}, {Name: "nginx", Enabled: true}}
			if tc.properties != nil {
				source.Properties = tc.properties
			}
			api := newPreflightTargetAPI(t, workspaceCatalog(tc.eligibility))
			options := preflightOwnerPlanOptions()
			options.Repository = RepositoryTargetPlan{GitIntegrationID: tc.integration}
			options.SkipCode = tc.skipCode
			options.WorkspaceInstances = map[string]bool{"inst-1": true}
			plan := preflightBuildPlan(t, export, options)
			prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{SkipData: true, SkipCode: tc.skipCode})
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Instances[0].Workspace != nil {
				t.Fatalf("blocked instance was prepared as a workspace: %#v", prepared.Instances[0].Workspace)
			}
			blockers := strings.Join(workspaceBlockers(plan), "\n")
			if !strings.Contains(blockers, "development workspace: ") || !strings.Contains(blockers, tc.want) {
				t.Fatalf("blockers do not explain %q:\n%s", tc.want, blockers)
			}
		})
	}
}

// The storage choice travels from the options through the reviewed plan into
// the creation request, the cron schedules are shown as disabled, and the
// capacity check counts the workspace's SSH runner.
func TestWorkspaceStorageCapacityAndCronPlan(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true, CronJobs: []CronJob{{Title: "cron", Crontab: "0 * * * *", Command: "drush cron", Enabled: true}}},
		{Name: "nginx", Enabled: true},
	}
	api := newPreflightTargetAPI(t, workspaceCatalog(eligibleWorkspace()))
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
	options.WorkspaceInstances = map[string]bool{"inst-1": true}
	options.WorkspaceStorageClass = "fast"
	plan := preflightBuildPlan(t, export, options)
	prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{SkipData: true})
	if err != nil {
		t.Fatal(err)
	}
	if blockers := workspaceBlockers(plan); len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %q", blockers)
	}
	_, input, _, err := workspaceCreationInput(prepared.Instances[0])
	if err != nil || input.StorageClassName != "fast" || input.StorageServiceName != "" {
		t.Fatalf("workspace storage = %#v, %v", input, err)
	}
	for _, service := range plan.Apps[0].Instances[0].Services {
		for _, cron := range service.CronSchedules {
			if cron.TargetState != "disabled in a development workspace" {
				t.Fatalf("cron target state = %q", cron.TargetState)
			}
		}
	}
	// php and nginx are enabled, and the runner is one more billed service.
	if len(api.capacityAdditional) != 1 || api.capacityAdditional[0] != 3 {
		t.Fatalf("capacity requests = %v, want one for 3 services", api.capacityAdditional)
	}

	options.WorkspaceStorageClass, options.WorkspaceStorageService = "", "files"
	plan = preflightBuildPlan(t, export, options)
	if _, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{SkipData: true}); err != nil {
		t.Fatal(err)
	}
	if blockers := strings.Join(workspaceBlockers(plan), "\n"); !strings.Contains(blockers, "--workspace-storage-service") {
		t.Fatalf("an unknown storage service must block the preview:\n%s", blockers)
	}
}

func TestPlanWithoutWorkspaceKeepsItsContent(t *testing.T) {
	export := preflightFixtureExport(true)
	plain := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
	options := preflightOwnerPlanOptions()
	options.WorkspaceInstances = map[string]bool{"another-instance": true}
	unselected := preflightBuildPlan(t, export, options)
	if !reflect.DeepEqual(plain, unselected) || plain.Apps[0].Instances[0].Workspace != nil {
		t.Fatal("an instance that is not selected must plan exactly as before")
	}
}

func TestWorkspaceCreationInput(t *testing.T) {
	integration, repo, ref, refType := 44, "remote-repo-17", "main", TargetGitRefBranch
	prepared := PreparedInstance{
		StackServices: []TargetStackServiceInspection{
			{StackService: TargetStackService{ID: 21, Name: "php"}},
			{StackService: TargetStackService{ID: 22, Name: "nginx"}},
			{StackService: TargetStackService{ID: 23, Name: "redis"}},
			{StackService: TargetStackService{ID: 24, Name: "solr", Disabled: true}},
		},
		EffectiveState: map[string]bool{"php": true, "nginx": true, "redis": false, "solr": true},
		Services: map[string]PreparedService{
			"php":   {Target: TargetStackServiceInspection{StackService: TargetStackService{Name: "php"}}, InstanceVersion: "8.3"},
			"redis": {Target: TargetStackServiceInspection{StackService: TargetStackService{Name: "redis"}}, InstanceVersion: "7"},
		},
		BuildSource: &PreparedBuildSource{ServiceName: "php", Input: TargetBuildSourceInput{
			BuildSourceType: TargetBuildSourceConnect, IntegrationID: &integration, RemoteGitRepoID: &repo, GitRef: &ref, GitRefType: &refType,
		}},
		Workspace: &PreparedWorkspace{Branch: "main", SourceServiceName: "php", CodeServiceNames: map[string]bool{"php": true, "nginx": true}},
	}
	_, _, overrides, err := workspaceCreationInput(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 3 {
		t.Fatalf("overrides = %#v", overrides)
	}
	php, redis, solr := overrides[0], overrides[1], overrides[2]
	if php.ID != 21 || php.BuildSource == nil || php.Version == nil || *php.Version != "8.3" || php.Disabled != nil {
		t.Fatalf("code service override = %#v", php)
	}
	// A supporting service keeps its version for the usual update after
	// creation; only its state is sent.
	if redis.ID != 23 || redis.Version != nil || redis.Disabled == nil || !*redis.Disabled {
		t.Fatalf("disabled supporting service override = %#v", redis)
	}
	if solr.ID != 24 || solr.Disabled == nil || *solr.Disabled {
		t.Fatalf("enabled supporting service override = %#v", solr)
	}

	if mode, workspace, overrides, err := workspaceCreationInput(PreparedInstance{}); err != nil || mode != "" || workspace != nil || overrides != nil {
		t.Fatal("a standard environment must send no workspace fields")
	}
	prepared.BuildSource = nil
	if _, _, _, err := workspaceCreationInput(prepared); err == nil {
		t.Fatal("a workspace without its repository must not be created")
	}
}

func TestValidatePreparedInstanceChecksExecutionMode(t *testing.T) {
	plan := InstancePlan{TargetEnvType: "DEV"}
	standard := PreparedInstance{Source: Instance{Name: "dev"}, Stack: TargetStack{RevID: 6}}
	workspace := standard
	workspace.Workspace = &PreparedWorkspace{Branch: "main"}
	item := TargetAppInstance{ID: 20, Name: "dev", AppID: 10, ClusterID: 3, EnvironmentType: "dev", StackRevID: 6}
	if err := validatePreparedInstance(item, 10, standard, plan, 3); err != nil {
		t.Fatal(err)
	}
	// An installation that ignored the workspace fields created a built environment.
	if err := validatePreparedInstance(item, 10, workspace, plan, 3); err == nil {
		t.Fatal("a standard environment must not pass for a planned workspace")
	}
	item.ExecutionMode = "workspace"
	item.Workspace = &TargetWorkspace{Branch: "main"}
	if err := validatePreparedInstance(item, 10, workspace, plan, 3); err != nil {
		t.Fatal(err)
	}
	if err := validatePreparedInstance(item, 10, standard, plan, 3); err == nil {
		t.Fatal("a workspace must not pass for a planned standard environment")
	}
	item.Workspace.Branch = "develop"
	if err := validatePreparedInstance(item, 10, workspace, plan, 3); err == nil {
		t.Fatal("a workspace on another branch must be rejected")
	}
}

func TestWaitWorkspaceReady(t *testing.T) {
	for _, tc := range []struct {
		name    string
		states  []TargetWorkspace
		wantErr string
	}{
		{name: "ready after setup", states: []TargetWorkspace{
			{}, {PreparationState: "pending"}, {PreparationState: "running", Initialized: true}, {PreparationState: "ready", Initialized: true},
		}},
		{name: "setup failed", states: []TargetWorkspace{
			{PreparationState: "running"}, {PreparationState: "failed", Error: "composer install exited with 2"},
		}, wantErr: "composer install exited with 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/app-environments/20" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				state := tc.states[requests]
				requests++
				writeTargetExecutionJSON(t, w, TargetAppInstance{
					ID: 20, Name: "dev", Status: "OK", AppID: 10, ClusterID: 3, EnvironmentType: "dev",
					StackID: 5, StackRevID: 6, ExecutionMode: "workspace", Workspace: &state,
				})
			}))
			defer server.Close()
			executor := &MigrationExecutor{
				target: mustTargetExecutionClient(t, server.URL), pollInterval: time.Millisecond, operationTimeout: time.Second,
			}
			err := executor.waitWorkspaceReady(context.Background(), 20)
			if tc.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr) ||
				!strings.Contains(err.Error(), "wodby app environment workspace prepare 20")) {
				t.Fatalf("error = %v", err)
			}
			if requests != len(tc.states) {
				t.Fatalf("status requests = %d, want %d", requests, len(tc.states))
			}
		})
	}
}

func TestWorkspaceDeploymentHasNoBuild(t *testing.T) {
	prepared := PreparedInstance{
		BuildSource: &PreparedBuildSource{ServiceName: "php"},
		Workspace:   &PreparedWorkspace{SourceServiceName: "php", CodeServiceNames: map[string]bool{"php": true}},
	}
	input, err := technicalDeploymentInput(prepared, []TargetAppService{{ID: 31, Name: "php"}, {ID: 32, Name: "sshd", Disabled: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Services) != 1 || input.Services[0].AppServiceID != 31 || input.Services[0].AppServiceBuildID != nil {
		t.Fatalf("workspace deployment = %#v", input.Services)
	}
}

// The whole apply and verify run for a workspace: the repository goes into the
// creation request, nothing is built, the code service is never updated, and
// the run waits for the workspace setup before it reports success.
func TestMigrationExecutorCreatesWorkspace(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	appCreated := false
	deploymentID := 30
	deployments := map[int]TargetAppDeployment{}
	app := TargetApp{ID: 10, Name: "demo", Title: "Demo", OrgID: 1, CreatedAt: now, UpdatedAt: now}
	setup := []TargetWorkspace{
		{SourceAppServiceID: 21, Branch: "main", PreparationState: "pending"},
		{SourceAppServiceID: 21, Branch: "main", PreparationState: "running"},
		{SourceAppServiceID: 21, Branch: "main", PreparationState: "ready", Initialized: true},
	}
	setupPolls := 0
	instance := func(workspace TargetWorkspace) TargetAppInstance {
		return TargetAppInstance{
			ID: 20, Name: "dev", Title: "Dev", Status: "OK", AppID: 10, ClusterID: 3, EnvironmentType: "dev",
			StackID: 5, StackRevID: 12, ExecutionMode: "workspace", Workspace: &workspace, CreatedAt: now, UpdatedAt: now,
		}
	}
	service := TargetAppService{ID: 21, Name: "php", Status: "OK", Replicas: 1, AppInstanceID: 20, ServiceRevID: 101, CreatedAt: now, UpdatedAt: now}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/stacks/5":
			writeTargetExecutionJSON(t, w, TargetStack{ID: 5, Name: "drupal", Status: "OK", RevID: 12, OrgID: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/stack-revisions/12":
			writeTargetExecutionJSON(t, w, TargetStackRevision{ID: 12, StackID: 5, Number: 1})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/stack-revisions/12/services":
			writeTargetExecutionJSON(t, w, []TargetStackService{{ID: 11, Name: "php", ServiceRevID: 101}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/service-revisions/101":
			writeTargetExecutionJSON(t, w, TargetServiceRevision{ID: 101, ServiceID: 201, Name: "php"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			if appCreated {
				writeTargetExecutionJSON(t, w, []TargetApp{app})
			} else {
				writeTargetExecutionJSON(t, w, []TargetApp{})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps":
			var body struct {
				ExecutionMode          string                          `json:"executionMode"`
				Workspace              *TargetNewWorkspaceInput        `json:"workspace"`
				ServiceOverrides       []TargetAppServiceOverrideInput `json:"serviceOverrides"`
				DeferInitialDeployment bool                            `json:"deferInitialDeployment"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ExecutionMode != "workspace" || body.Workspace == nil || body.Workspace.Branch != "main" || !body.DeferInitialDeployment {
				t.Fatalf("workspace creation body = %#v", body)
			}
			if len(body.ServiceOverrides) != 1 || body.ServiceOverrides[0].ID != 11 || body.ServiceOverrides[0].BuildSource == nil ||
				*body.ServiceOverrides[0].BuildSource.RemoteGitRepoID != "remote-repo-17" {
				t.Fatalf("workspace repository must be sent at creation: %#v", body.ServiceOverrides)
			}
			appCreated = true
			writeTargetExecutionJSON(t, w, app)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-environments":
			writeTargetExecutionJSON(t, w, []TargetAppInstance{instance(setup[0])})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-environments/20":
			state := setup[len(setup)-1]
			if len(deployments) == 0 {
				state = setup[0]
			} else if setupPolls < len(setup) {
				state = setup[setupPolls]
				setupPolls++
			}
			writeTargetExecutionJSON(t, w, instance(state))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-services":
			writeTargetExecutionJSON(t, w, []TargetAppService{service})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-ports":
			writeTargetExecutionJSON(t, w, []TargetAppPort{})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-routes":
			writeTargetExecutionJSON(t, w, []TargetAppRoute{})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/app-auths":
			writeTargetExecutionJSON(t, w, []TargetAppAuth{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/app-deployments":
			var body TargetCreateAppDeploymentInput
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Services) != 1 || body.Services[0].AppServiceID != service.ID || body.Services[0].AppServiceBuildID != nil {
				t.Fatalf("a workspace is deployed without a build: %#v", body)
			}
			deploymentID++
			item := TargetAppDeployment{
				ID: deploymentID, Status: "COMPLETED", AppInstanceID: 20, PostDeploymentStatus: "skipped", CreatedAt: now, UpdatedAt: now,
				AppServiceDeployments: []TargetAppServiceDeployment{{
					ID: deploymentID + 100, Status: "COMPLETED", AppServiceID: service.ID, Force: true, CreatedAt: now, UpdatedAt: now,
				}},
			}
			deployments[item.ID] = item
			writeTargetExecutionJSON(t, w, item)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/app-deployments/"):
			id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/v1/app-deployments/"))
			if err != nil {
				t.Fatal(err)
			}
			writeTargetExecutionJSON(t, w, deployments[id])
		default:
			// Builds and code service updates are requests a workspace rejects.
			t.Fatalf("unexpected target request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	sourceInstance := Instance{
		UUID: "instance-1", Name: "dev", Title: "Dev", Type: "dev", Status: "ok",
		Stack:      Stack{UUID: "stack-1", Name: "drupal", Version: "1"},
		Properties: map[string]interface{}{"deployment_type": "git", "git_target_value": "main", "git_target_type": "branch"},
		Services:   []Service{{Name: "php", Enabled: true}},
	}
	export := Export{
		Schema: ExportSchemaV2, Source: &ExportSource{Kind: "app", UUID: "app-1"}, SecretsIncluded: true,
		Apps: []AppExport{{
			App:       App{UUID: "app-1", Name: "demo", Title: "Demo", Type: "app", Status: "ok"},
			Instances: []Instance{sourceInstance},
		}},
	}
	configDigest, err := export.ConfigDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{Kind: "app", ID: "app-1", Schema: ExportSchemaV2, ConfigDigest: configDigest},
		Target: PlanTarget{OrgID: 1, ClusterID: 3, OrgOwnerOrAdminVerified: true, DiscoveryVerified: true},
		Apps: []AppPlan{{
			SourceUUID: "app-1", Name: "demo",
			Instances: []InstancePlan{{
				SourceUUID: "instance-1", Name: "dev", TargetEnvID: 1, TargetEnvType: "dev",
				Stack:     StackPlan{Target: "drupal", TargetID: 5, TargetRevID: 12},
				Workspace: &WorkspacePlan{Branch: "main"},
			}},
		}},
		Status: "target_scope_validated",
	}
	plan.PlanHash, err = plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	integration, repo, ref, refType := 44, "remote-repo-17", "main", TargetGitRefBranch
	stackService := TargetStackServiceInspection{StackService: TargetStackService{ID: 11, Name: "php", ServiceRevID: 101}}
	two := 2
	prepared := PreparedMigration{
		App: export.Apps[0],
		Instances: []PreparedInstance{{
			Source:        sourceInstance,
			Stack:         TargetStack{ID: 5, Name: "drupal", RevID: 12, OrgID: 1},
			StackServices: []TargetStackServiceInspection{stackService},
			// Wodby 1 ran two replicas; the workspace keeps its single one.
			Services:          map[string]PreparedService{"php": {Source: sourceInstance.Services[0], Target: stackService, Replicas: &two}},
			Imports:           map[string]PreparedImport{},
			ImportByComponent: map[string]PreparedImport{},
			EffectiveState:    map[string]bool{"php": true},
			BuildSource: &PreparedBuildSource{ServiceName: "php", Input: TargetBuildSourceInput{
				BuildSourceType: TargetBuildSourceConnect, IntegrationID: &integration, RemoteGitRepoID: &repo, GitRef: &ref, GitRefType: &refType,
			}},
			Workspace: &PreparedWorkspace{Branch: "main", SourceServiceName: "php", CodeServiceNames: map[string]bool{"php": true}},
		}},
	}
	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	progress := []string{}
	executor, err := NewMigrationExecutor(client, MigrationExecutorOptions{
		StatePath: filepath.Join(t.TempDir(), "state.json"), PollInterval: time.Millisecond, OperationTimeout: time.Second,
		Progress: func(message string) { progress = append(progress, message) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := executor.Apply(ctx, export, plan, prepared); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if setupPolls != len(setup) {
		t.Fatalf("apply must wait for the workspace setup: %d of %d states read", setupPolls, len(setup))
	}
	result, err := executor.Verify(ctx, export, plan, prepared, TargetCluster{ID: 3, OrgID: 1, Status: "OK", IPs: []string{"203.0.113.10"}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.State.Status != MigrationStatusComplete || len(deployments) != 1 {
		t.Fatalf("result status=%q deployments=%d", result.State.Status, len(deployments))
	}
	output := strings.Join(progress, "\n")
	for _, expected := range []string{
		`Step: set up the workspace of target app environment "dev" (ID 20).`,
		`is being set up (running)`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("progress output does not contain %q:\n%s", expected, output)
		}
	}
}
