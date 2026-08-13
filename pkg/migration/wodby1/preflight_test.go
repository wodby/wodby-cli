package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestPreflightTargetResolvesOfficialStackServicesAndRehashesPlan(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true},
		{Name: "mariadb", Enabled: true},
		{Name: "files-nfs", Enabled: true},
	}
	export.Apps[0].Instances[0].Backups = []Backup{
		{
			UUID: "backup-file-db", BackupUUID: "backup-db", Component: "db",
			URL:    "https://backups.example.test/database.sql.gz?signature=secret",
			Status: "ok", Size: 101, BackupCreated: 1001,
		},
		{
			UUID: "backup-file-files", BackupUUID: "backup-files", Component: "files",
			URL:    "https://backups.example.test/files.tar.gz?signature=secret",
			Status: "ok", Size: 202, BackupCreated: 1002,
		},
	}

	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{
		GitIntegrationID: 44,
	}
	plan := preflightBuildPlan(t, export, options)
	hashBefore := plan.PlanHash
	if hashBefore == "" {
		t.Fatal("base plan hash is empty")
	}

	api := newPreflightTargetAPI(t, preflightOfficialCatalog())
	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Status == "blocked" || plan.Summary.Blocking != 0 {
		t.Fatalf("preflight unexpectedly blocked: status=%q review=%#v", plan.Status, plan.Review)
	}
	if len(plan.PlanHash) != 64 || plan.PlanHash == hashBefore {
		t.Fatalf("preflight plan hash = %q, base hash = %q", plan.PlanHash, hashBefore)
	}
	recalculated, err := plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanHash != recalculated {
		t.Fatalf("plan hash = %q, recalculated = %q", plan.PlanHash, recalculated)
	}

	instancePlan := &plan.Apps[0].Instances[0]
	if instancePlan.Stack.Target != "acme/drupal11" ||
		instancePlan.Stack.TargetID != 7 ||
		instancePlan.Stack.TargetRevID != 71 ||
		instancePlan.Stack.TargetVersion != "revision-4" {
		t.Fatalf("resolved stack plan = %#v", instancePlan.Stack)
	}
	assertPreflightServicePlan(t, instancePlan, "php", "php", 11, 101)
	assertPreflightServicePlan(t, instancePlan, "mariadb", "mariadb", 12, 102)
	assertPreflightServicePlan(t, instancePlan, "files-nfs", "files-nfs", 13, 103)
	assertPreflightImportPlan(t, instancePlan, "db", "mariadb", "database", 12)
	assertPreflightImportPlan(t, instancePlan, "files", "files-nfs", "files", 13)
	if instancePlan.BuildServiceID != 11 ||
		instancePlan.BuildServiceRevID != 101 {
		t.Fatalf("instance build service pins = %#v", instancePlan)
	}
	if repository := plan.Apps[0].Repository; repository == nil ||
		repository.RepositoryName != "acme/example" ||
		repository.RemoteGitRepoID != "remote-repo-17" {
		t.Fatalf("resolved repository = %#v", repository)
	}
	if !hasReviewMessage(plan.Review, SeverityMigration, `Wodby CI will be used by default; pipeline ".wodby/pipeline.yml" was found in repository "acme/example" at Git branch "main"`) {
		t.Fatalf("successful Wodby CI pipeline validation is not disclosed: %#v", plan.Review)
	}
	if !hasReviewMessage(plan.Review, SeverityConfirmation, wodby1LegacyEnvVarsMarker+" will be enabled on migrated stack services") {
		t.Fatalf("legacy Wodby 1 environment compatibility warning is not disclosed: %#v", plan.Review)
	}
	for serviceName, configuration := range prepared.StackConfiguration.Services {
		found := false
		for _, variable := range configuration.EnvVars {
			if variable.Name == wodby1LegacyEnvVarsMarker && variable.Value == "true" && variable.EnvType == nil {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("prepared stack service %q is missing the legacy compatibility marker: %#v", serviceName, configuration.EnvVars)
		}
	}
	for _, item := range plan.Review {
		if item.Subject == "new target stack" {
			t.Fatalf("explicit target stack produced a catalog creation warning: %#v", item)
		}
	}

	if len(prepared.Instances) != 1 {
		t.Fatalf("prepared instances = %#v", prepared.Instances)
	}
	preparedInstance := prepared.Instances[0]
	if preparedInstance.Stack.ID != 7 || preparedInstance.Stack.RevID != 71 {
		t.Fatalf("prepared stack = %#v", preparedInstance.Stack)
	}
	if len(preparedInstance.Services) != 3 {
		t.Fatalf("prepared services = %#v", preparedInstance.Services)
	}
	if preparedInstance.BuildSource == nil ||
		preparedInstance.BuildSource.ServiceName != "php" ||
		preparedInstance.BuildSource.Input.BuildSourceType != TargetBuildSourceConnect ||
		preparedInstance.BuildSource.Input.IntegrationID == nil ||
		*preparedInstance.BuildSource.Input.IntegrationID != 44 ||
		preparedInstance.BuildSource.Input.RemoteGitRepoID == nil ||
		*preparedInstance.BuildSource.Input.RemoteGitRepoID != "remote-repo-17" ||
		preparedInstance.BuildSource.Input.GitRef == nil ||
		*preparedInstance.BuildSource.Input.GitRef != "main" ||
		preparedInstance.BuildSource.Input.GitRefType == nil ||
		*preparedInstance.BuildSource.Input.GitRefType != TargetGitRefBranch {
		t.Fatalf("prepared build source = %#v", preparedInstance.BuildSource)
	}
	if len(preparedInstance.Imports) != 2 ||
		preparedInstance.Imports["backup-file-db"].ServiceName != "mariadb" ||
		preparedInstance.Imports["backup-file-db"].ImportName != "database" ||
		preparedInstance.Imports["backup-file-files"].ServiceName != "files-nfs" ||
		preparedInstance.Imports["backup-file-files"].ImportName != "files" {
		t.Fatalf("prepared imports = %#v", preparedInstance.Imports)
	}

	changedPlan := preflightClonePlan(t, plan)
	changedPlan.Apps[0].Instances[0].Stack.TargetRevID++
	changedHash, err := changedPlan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == plan.PlanHash {
		t.Fatal("approval hash does not bind the resolved target stack revision ID")
	}
	changedPlan = preflightClonePlan(t, plan)
	changedPlan.Apps[0].Instances[0].Stack.Target = "drupal11"
	changedHash, err = changedPlan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == plan.PlanHash {
		t.Fatal("approval hash does not bind the actual resolved target stack name")
	}

	wantPaths := []string{
		"/v1/apps",
		"/v1/integrations/44/options/remote-git-repos",
		"/v1/stacks/7",
		"/v1/stack-revisions/71",
		"/v1/stack-revisions/71/services",
		"/v1/service-revisions/103",
		"/v1/service-revisions/102",
		"/v1/service-revisions/101",
		"/v1/integrations/44/options/remote-git-repo-file",
	}
	if got := api.requestPaths(); !equalPreflightStrings(got, wantPaths) {
		t.Fatalf("target API paths = %#v, want %#v", got, wantPaths)
	}
}

func TestPreflightTargetSelectsPublicCatalogStackForManagedAppByDefault(t *testing.T) {
	export := preflightFixtureExport(false)
	options := preflightOwnerPlanOptions()
	options.TargetStackID = 0
	options.SkipCode = true
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	if !plan.Apps[0].Instances[0].Stack.CreateTarget || plan.Apps[0].Instances[0].Stack.CatalogName != "drupal11" {
		t.Fatalf("default stack plan = %#v", plan.Apps[0].Instances[0].Stack)
	}

	catalog := preflightOfficialCatalog()
	public := catalog.stacks["drupal11"]
	public.ID = 5
	public.Name = "drupal11"
	public.Public = true
	public.OrgID = 1
	public.OriginStackRevName = nil
	catalog.publicStacks = []TargetStack{public}
	catalog.stackRevisions[71] = TargetStackRevision{ID: 71, Name: "drupal11", Number: 4, Version: "11", StackID: 5}
	api := newPreflightTargetAPI(t, catalog)
	_, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{SkipCode: true, SkipData: true})
	if err != nil {
		t.Fatal(err)
	}
	stack := plan.Apps[0].Instances[0].Stack
	if stack.TargetID != 5 || stack.TargetRevID != 71 || stack.Target != "drupal11" || !stack.CreateTarget {
		t.Fatalf("preflighted catalog stack = %#v", stack)
	}
	for _, item := range plan.Review {
		if item.Subject == "new target stack" || item.Subject == "target stack revision" {
			t.Fatalf("deterministic catalog stack selection should be shown in the migration table, not as a review item: %#v", item)
		}
	}
}

func TestPreflightTargetWarnsAndUsesCustomCIWhenRepositoryIsMissingFromIntegration(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{
		GitIntegrationID: 44,
		RepositoryName:   "acme/renamed",
	}
	plan := preflightBuildPlan(t, export, options)
	catalog := preflightSingleBuildCatalog("php", false)
	catalog.remoteGitRepos = map[int][]TargetRemoteGitRepo{
		44: {{ID: "remote-repo-17", Name: "acme/example"}},
	}
	api := newPreflightTargetAPI(t, catalog)

	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Instances[0].BuildSource == nil || prepared.Instances[0].BuildSource.ServiceName != "php" ||
		prepared.Instances[0].BuildSource.Input.BuildSourceType != "" || !prepared.Instances[0].ExternalCIOnly {
		t.Fatalf("missing repository Custom CI preparation = %#v", prepared.Instances[0])
	}
	if plan.Apps[0].Repository.RemoteGitRepoID != "" ||
		plan.Apps[0].Repository.Action != "unlinked" ||
		!preflightHasReview(plan, SeverityServiceWarning, "repository", "continue without a Git link") {
		t.Fatalf("missing repository review = %#v", plan.Review)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("missing optional repository blocked migration: %#v", plan.Review)
	}
}

func TestPreflightTargetBlocksMissingWodbyCIPipelineWithStackGuidance(t *testing.T) {
	tests := []struct {
		name      string
		appType   string
		stackName string
		links     []string
	}{
		{
			name:      "Drupal",
			appType:   "drupal",
			stackName: "drupal11",
			links: []string{
				"https://github.com/wodby/drupal-vanilla/blob/11.x/.wodby/pipeline.yml",
				"https://github.com/wodby/drupal-vanilla/blob/11.x/.wodby/post-deployment.yml",
			},
		},
		{
			name:      "WordPress",
			appType:   "wordpress",
			stackName: "wordpress",
			links: []string{
				"https://github.com/wodby/wordpress-vanilla/blob/main/.wodby/pipeline.yml",
				"https://github.com/wodby/wordpress-vanilla/blob/main/.wodby/post-deployment.yml",
				"https://github.com/wodby/wordpress-vanilla/tree/main/.wodby",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			export := preflightFixtureExport(true)
			export.Apps[0].App.Type = test.appType
			export.Apps[0].Instances[0].Stack.Name = test.stackName
			export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
			options := preflightOwnerPlanOptions()
			options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
			options.SkipData = true
			plan := preflightBuildPlan(t, export, options)
			catalog := preflightSingleBuildCatalog("php", false)
			catalog.remoteGitRepoFiles = map[string]bool{
				preflightRemoteGitRepoFileKey(44, "remote-repo-17", wodbyCIPipelinePath, "main"): false,
			}
			api := newPreflightTargetAPI(t, catalog)

			_, err := api.client.PreflightTarget(
				context.Background(),
				export,
				&plan,
				TargetPreflightOptions{SkipData: true},
			)
			if err != nil {
				t.Fatal(err)
			}
			var message string
			for _, item := range plan.Review {
				if item.Severity == SeverityBlocking && item.Subject == "Wodby CI pipeline" {
					message = item.Message
					break
				}
			}
			if message == "" || !strings.Contains(message, wodbyCIPipelinePath) ||
				!strings.Contains(message, `branch "main"`) {
				t.Fatalf("missing pipeline review = %#v", plan.Review)
			}
			for _, link := range test.links {
				if !strings.Contains(message, link) {
					t.Fatalf("pipeline review %q does not contain %q", message, link)
				}
			}
		})
	}
}

func TestPreflightTargetDoesNotRequireWodbyCIPipelineForThirdPartyCI(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
	options.TargetCIIntegrationID = 72
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	api := newPreflightTargetAPI(t, preflightSingleBuildCatalog("php", false))

	_, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if preflightHasReview(plan, SeverityBlocking, "Wodby CI pipeline", "") {
		t.Fatalf("third-party CI unexpectedly requires a Wodby CI pipeline: %#v", plan.Review)
	}
	for _, path := range api.requestPaths() {
		if strings.HasSuffix(path, "/options/remote-git-repo-file") {
			t.Fatalf("third-party CI performed Wodby CI pipeline lookup: %#v", api.requestPaths())
		}
	}
}

func TestPreflightTargetUsesReviewedRevisionAfterLatestRevisionChanges(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = nil
	options := preflightOwnerPlanOptions()
	options.SkipCode = true
	options.SkipData = true

	initialAPI := newPreflightTargetAPI(t, preflightOfficialCatalog())
	reviewed := preflightBuildPlan(t, export, options)
	if _, err := initialAPI.client.PreflightTarget(
		context.Background(),
		export,
		&reviewed,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err != nil {
		t.Fatal(err)
	}
	if reviewed.Apps[0].Instances[0].Stack.TargetRevID != 71 {
		t.Fatalf("reviewed stack = %#v", reviewed.Apps[0].Instances[0].Stack)
	}

	current := preflightBuildPlan(t, export, options)
	if err := PinReviewedTargets(&current, reviewed); err != nil {
		t.Fatal(err)
	}
	changedCatalog := preflightOfficialCatalog()
	stack := changedCatalog.stacks["drupal11"]
	stack.RevID = 72
	stack.LatestRevNumber = 5
	changedCatalog.stacks["drupal11"] = stack
	changedCatalog.stackRevisions[72] = TargetStackRevision{
		ID: 72, Name: "drupal11", Number: 5, Version: "11", StackID: 7,
	}
	changedCatalog.stackServices[72] = []TargetStackService{}
	changedAPI := newPreflightTargetAPI(t, changedCatalog)

	prepared, err := changedAPI.client.PreflightTarget(
		context.Background(),
		export,
		&current,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if current.PlanHash != reviewed.PlanHash {
		t.Fatalf("current plan hash = %q, reviewed = %q", current.PlanHash, reviewed.PlanHash)
	}
	if got := prepared.Instances[0].Stack.RevID; got != 71 {
		t.Fatalf("prepared stack revision = %d, want reviewed revision 71", got)
	}
	wantPaths := []string{
		"/v1/apps",
		"/v1/stacks/7",
		"/v1/stack-revisions/71",
		"/v1/stack-revisions/71/services",
		"/v1/service-revisions/103",
		"/v1/service-revisions/102",
		"/v1/service-revisions/101",
	}
	if got := changedAPI.requestPaths(); !equalPreflightStrings(got, wantPaths) {
		t.Fatalf("target API paths = %#v, want exact reviewed reads %#v", got, wantPaths)
	}
}

func TestPreflightTargetBlocksUnrelatedAppNameCollisionButAllowsStateBackedApp(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = nil
	options := preflightOwnerPlanOptions()
	catalog := preflightOfficialCatalog()
	catalog.apps = []TargetApp{{ID: 91, Name: "example", OrgID: 8}}
	api := newPreflightTargetAPI(t, catalog)

	blocked := preflightBuildPlan(t, export, options)
	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&blocked,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err != nil {
		t.Fatal(err)
	}
	if !preflightHasReview(blocked, SeverityBlocking, "target app name", "already contains app") {
		t.Fatalf("collision review = %#v", blocked.Review)
	}
	if !preflightHasReview(blocked, SeverityBlocking, "target app name", `instance(s) "prod"`) {
		t.Fatalf("collision review does not identify the blocked target instance: %#v", blocked.Review)
	}

	resumed := preflightBuildPlan(t, export, options)
	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&resumed,
		TargetPreflightOptions{
			SkipCode:           true,
			SkipData:           true,
			AllowedTargetAppID: 91,
		},
	); err != nil {
		t.Fatal(err)
	}
	if preflightHasReview(resumed, SeverityBlocking, "target app name", "") {
		t.Fatalf("state-backed app was treated as a collision: %#v", resumed.Review)
	}
}

func TestPreflightTargetPreparesEveryServerAppWithPerAppRecovery(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Source = &ExportSource{Kind: "server", UUID: "server-1"}
	export.Apps[0].Instances[0].Services = nil
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
	catalog := preflightOfficialCatalog()
	catalog.apps = []TargetApp{{ID: 91, Name: "second", OrgID: 8}}
	api := newPreflightTargetAPI(t, catalog)
	progress := []TargetPreflightProgress{}

	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{
			SkipCode:            true,
			SkipData:            true,
			AllowedTargetAppIDs: map[string]int{"app-2": 91},
			Progress: func(event TargetPreflightProgress) {
				progress = append(progress, event)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Apps) != 2 || prepared.App.App.UUID != "" || len(prepared.Instances) != 0 {
		t.Fatalf("prepared server migration = %#v", prepared)
	}
	for _, appUUID := range []string{"app-1", "app-2"} {
		child, found := prepared.ForApp(appUUID)
		if !found || child.App.App.UUID != appUUID || len(child.Instances) != 1 {
			t.Fatalf("prepared child %q = %#v, found=%t", appUUID, child, found)
		}
	}
	if preflightHasReview(plan, SeverityBlocking, "target app name", "") {
		t.Fatalf("state-backed server app was treated as a collision: %#v", plan.Review)
	}
	if len(progress) != 6 {
		t.Fatalf("preflight progress = %#v, want app/instance/completion for two apps", progress)
	}
	for index, stage := range []string{"app", "instance", "app_complete", "app", "instance", "app_complete"} {
		if progress[index].Stage != stage {
			t.Fatalf("preflight progress[%d] = %#v, want stage %q", index, progress[index], stage)
		}
	}
	if progress[0].AppIndex != 1 || progress[0].AppTotal != 2 || progress[0].InstanceTotal != 1 ||
		progress[3].AppIndex != 2 || progress[4].InstanceIndex != 1 {
		t.Fatalf("preflight progress counters = %#v", progress)
	}
}

func TestPreflightTargetRequiresVerifiedOrgOwnerOrAdminPlan(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = nil
	catalog := preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{71: {}},
		revisions:     map[int]TargetServiceRevision{},
	}
	api := newPreflightTargetAPI(t, catalog)

	ownerOptions := preflightOwnerPlanOptions()
	ownerPlan := preflightBuildPlan(t, export, ownerOptions)
	if ownerPlan.Target.OrgRole != "owner" ||
		!ownerPlan.Target.OrgOwnerOrAdminVerified ||
		!ownerPlan.Target.DiscoveryVerified {
		t.Fatalf("owner target plan = %#v", ownerPlan.Target)
	}
	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&ownerPlan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err != nil {
		t.Fatalf("verified organization owner was rejected: %v", err)
	}
	requestsAfterOwner := len(api.requestPaths())

	memberOptions := preflightOwnerPlanOptions()
	memberOptions.TargetScope.User.IsAdmin = true
	memberOptions.TargetScope.Membership.Role = "member"
	memberPlan := preflightBuildPlan(t, export, memberOptions)
	if memberPlan.Target.OrgOwnerOrAdminVerified {
		t.Fatalf("platform admin/member was treated as an organization admin: %#v", memberPlan.Target)
	}
	if !preflightHasReview(
		memberPlan,
		SeverityBlocking,
		"target authorization",
		"organization owner or administrator",
	) {
		t.Fatalf("member plan lacks authorization blocker: %#v", memberPlan.Review)
	}
	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&memberPlan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err == nil || !strings.Contains(err.Error(), "organization owner/admin discovery") {
		t.Fatalf("member preflight error = %v", err)
	}
	if got := len(api.requestPaths()); got != requestsAfterOwner {
		t.Fatalf("rejected member plan made %d target requests, want %d", got, requestsAfterOwner)
	}

	unverifiedOptions := PlanOptions{
		SourceKind:                    "app",
		SourceID:                      "app-1",
		TargetOrgOwnerOrAdminVerified: true,
	}
	unverifiedPlan := preflightBuildPlan(t, export, unverifiedOptions)
	if unverifiedPlan.Target.DiscoveryVerified {
		t.Fatalf("unverified plan = %#v", unverifiedPlan.Target)
	}
	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&unverifiedPlan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err == nil || !strings.Contains(err.Error(), "organization owner/admin discovery") {
		t.Fatalf("unverified preflight error = %v", err)
	}
	if got := len(api.requestPaths()); got != requestsAfterOwner {
		t.Fatalf("rejected unverified plan made %d target requests, want %d", got, requestsAfterOwner)
	}
}

func TestPreflightTargetHonorsExplicitCustomStackAndServiceMappings(t *testing.T) {
	export := preflightFixtureExport(false)
	source := &export.Apps[0].Instances[0]
	source.Stack = Stack{
		UUID: "custom-stack-uuid", Name: "customer-stack", Version: "private-8",
		Custom: true, AncestorUUID: "drupal11-uuid", AncestorName: "drupal11",
	}
	source.Services = []Service{{
		Name: "legacy-web", Version: "7.3", Enabled: true,
		EnvVars: []EnvVar{{Name: "FORK_ONLY_DEFAULT", Value: "yes", Enabled: true, Origin: "custom_stack"}},
	}, {Name: "optional-extra", Enabled: true}}

	options := preflightOwnerPlanOptions()
	options.TargetStackMap = map[string]string{
		"inst-1": "17",
	}
	options.TargetStackID = 0
	options.TargetServiceMap = map[string]string{
		"inst-1/legacy-web": "php",
	}
	options.SkipCode = true
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	if plan.Apps[0].Instances[0].Stack.TargetID != 17 ||
		!plan.Apps[0].Instances[0].Stack.ExplicitMapping {
		t.Fatalf("custom target stack = %#v", plan.Apps[0].Instances[0].Stack)
	}
	if service := preflightFindServicePlan(t, &plan.Apps[0].Instances[0], "legacy-web"); service.TargetName != "php" ||
		service.Action != "map" {
		t.Fatalf("custom service plan = %#v", service)
	}

	catalog := preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"acme/drupal11": {
				ID: 17, Name: "acme/drupal11", RevID: 171, LatestRevNumber: 9, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{
			171: {
				{ID: 21, Name: "php", Required: true, ServiceRevID: 201},
				{ID: 22, Name: "optional-extra", ServiceRevID: 202},
			},
		},
		revisions: map[int]TargetServiceRevision{
			201: {
				ID: 201, ServiceID: 301, Name: "php",
				Manifest: &TargetServiceManifest{
					Name: "php", Build: &TargetServiceBuildCapability{Connect: true},
					Options: []TargetServiceOption{{Version: "8.3", Default: true}, {Version: "8.4"}},
				},
			},
			202: {
				ID: 202, ServiceID: 302, Name: "optional-extra",
				Manifest: &TargetServiceManifest{Name: "optional-extra"},
			},
		},
	}
	api := newPreflightTargetAPI(t, catalog)
	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("custom mapping blockers = %#v", plan.Review)
	}
	instancePlan := &plan.Apps[0].Instances[0]
	if instancePlan.Stack.TargetID != 17 || instancePlan.Stack.TargetRevID != 171 {
		t.Fatalf("custom stack IDs = %#v", instancePlan.Stack)
	}
	assertPreflightServicePlan(t, instancePlan, "legacy-web", "php", 21, 201)
	servicePlan := preflightFindServicePlan(t, instancePlan, "legacy-web")
	if servicePlan.SourceVersion != "7.3" || servicePlan.TargetVersion != "8.4" ||
		servicePlan.VersionAction != versionActionUpgrade || servicePlan.EnvVars != 1 {
		t.Fatalf("forked managed service migration plan = %#v", servicePlan)
	}
	mapped, found := prepared.Instances[0].Services["legacy-web"]
	if !found || mapped.Target.StackService.Name != "php" {
		t.Fatalf("prepared mapped service = %#v, found=%t", mapped, found)
	}
	extra, found := prepared.Instances[0].Services["optional-extra"]
	if !found || extra.Target.StackService.Name != "optional-extra" {
		t.Fatalf("prepared fork-only service = %#v, found=%t", extra, found)
	}
}

func TestPreflightTargetAcceptsBuiltInServiceCompatibilityMappings(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Properties = map[string]interface{}{"cache_redis": false}
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true},
		{Name: "apache", Enabled: true},
		{Name: "athenapdf", Enabled: true},
		{Name: "crond", Enabled: true},
		{Name: "mailhog", Enabled: true},
		{Name: "nginx", Enabled: true},
		{Name: "pma", Enabled: true},
		{Name: "redis", Enabled: true},
		{Name: "rsyslog", Enabled: true},
		{Name: "sshd", Enabled: true},
		{Name: "varnish", Enabled: true},
		{Name: "xhprof", Enabled: true},
	}
	options := preflightOwnerPlanOptions()
	options.SkipCode = true
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	catalog := preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{
			71: {
				{ID: 18, Name: "php", Required: true, ServiceRevID: 108},
				{ID: 11, Name: "nginx", Required: true, ServiceRevID: 101},
				{ID: 12, Name: "valkey", ServiceRevID: 102},
				{ID: 13, Name: "vinyl", ServiceRevID: 103},
				{ID: 14, Name: "gotenberg", ServiceRevID: 104},
				{ID: 15, Name: "mailpit", ServiceRevID: 105},
				{ID: 16, Name: "phpmyadmin", ServiceRevID: 106},
				{ID: 17, Name: "sshd", Type: "ssh", ServiceRevID: 107},
			},
		},
		revisions: map[int]TargetServiceRevision{
			108: {ID: 108, ServiceID: 208, Name: "php", Manifest: &TargetServiceManifest{Name: "php", Links: []TargetServiceLinkCapability{{Name: "sendmail"}}}},
			101: {ID: 101, ServiceID: 201, Name: "nginx", Manifest: &TargetServiceManifest{Name: "nginx"}},
			102: {ID: 102, ServiceID: 202, Name: "valkey", Manifest: &TargetServiceManifest{Name: "valkey"}},
			103: {ID: 103, ServiceID: 203, Name: "vinyl", Manifest: &TargetServiceManifest{Name: "vinyl"}},
			104: {ID: 104, ServiceID: 204, Name: "gotenberg", Type: "service", Manifest: &TargetServiceManifest{Name: "gotenberg"}},
			105: {ID: 105, ServiceID: 205, Name: "mailpit", Type: "service", Manifest: &TargetServiceManifest{Name: "mailpit"}},
			106: {ID: 106, ServiceID: 206, Name: "phpmyadmin", Type: "service", Manifest: &TargetServiceManifest{Name: "phpmyadmin"}},
			107: {ID: 107, ServiceID: 207, Name: "drupal11-php", Type: "service", Manifest: &TargetServiceManifest{Name: "drupal11-php"}},
		},
	}
	api := newPreflightTargetAPI(t, catalog)
	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("compatibility mapping blockers = %#v", plan.Review)
	}
	instancePlan := &plan.Apps[0].Instances[0]
	if service := preflightFindServicePlan(t, instancePlan, "apache"); service.Action != "skip" || service.TargetName != "" {
		t.Fatalf("apache plan = %#v", service)
	}
	assertPreflightServicePlan(t, instancePlan, "athenapdf", "gotenberg", 14, 104)
	if service := preflightFindServicePlan(t, instancePlan, "crond"); service.Action != "skip" || service.TargetName != "" {
		t.Fatalf("crond plan = %#v", service)
	}
	assertPreflightServicePlan(t, instancePlan, "mailhog", "mailpit", 15, 105)
	assertPreflightServicePlan(t, instancePlan, "php", "php", 18, 108)
	assertPreflightServicePlan(t, instancePlan, "nginx", "nginx", 11, 101)
	assertPreflightServicePlan(t, instancePlan, "pma", "phpmyadmin", 16, 106)
	assertPreflightServicePlan(t, instancePlan, "redis", "valkey", 12, 102)
	if preflightFindServicePlan(t, instancePlan, "redis").Enabled {
		t.Fatalf("redis plan should disable mapped valkey: %#v", preflightFindServicePlan(t, instancePlan, "redis"))
	}
	assertPreflightServicePlan(t, instancePlan, "sshd", "sshd", 17, 107)
	assertPreflightServicePlan(t, instancePlan, "varnish", "vinyl", 13, 103)
	preparedInstance := prepared.Instances[0]
	if len(preparedInstance.Services) != 8 ||
		preparedInstance.Services["athenapdf"].Target.StackService.Name != "gotenberg" ||
		preparedInstance.Services["mailhog"].Target.StackService.Name != "mailpit" ||
		preparedInstance.Services["pma"].Target.StackService.Name != "phpmyadmin" ||
		preparedInstance.Services["redis"].Target.StackService.Name != "valkey" ||
		preparedInstance.Services["sshd"].Target.StackService.Name != "sshd" ||
		preparedInstance.Services["varnish"].Target.StackService.Name != "vinyl" {
		t.Fatalf("prepared compatibility services = %#v", preparedInstance.Services)
	}
	links := prepared.StackConfiguration.Services["php"].Links
	if len(links) != 1 || links[0].Name != "sendmail" || links[0].LinkedServiceName != "mailpit" {
		t.Fatalf("stack-wide mail delivery link = %#v", links)
	}
	if preparedInstance.EffectiveState["valkey"] {
		t.Fatalf("cache_redis=false should disable target valkey: %#v", preparedInstance.EffectiveState)
	}
}

func TestPreflightTargetUsesPHPParentAsBuildServiceInsteadOfSSHDerivative(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true},
		{Name: "sshd", Enabled: true},
	}
	export.Apps[0].Instances[0].Properties = map[string]interface{}{
		"git_target_value": "main",
		"git_target_type":  "branch",
		"deployment_type":  "git",
	}
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{
		GitIntegrationID: 44,
	}
	plan := preflightBuildPlan(t, export, options)
	manifest := &TargetServiceManifest{
		Name:  "drupal11-php",
		Build: &TargetServiceBuildCapability{Connect: true},
	}
	catalog := preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8},
		},
		stackServices: map[int][]TargetStackService{
			71: {
				{ID: 11, Name: "php", Type: "service", Required: true, ServiceRevID: 101},
				{ID: 12, Name: "sshd", Type: "ssh", ServiceRevID: 101},
			},
		},
		revisions: map[int]TargetServiceRevision{
			101: {ID: 101, ServiceID: 201, Name: "drupal11-php", Type: "service", Manifest: manifest},
		},
	}
	api := newPreflightTargetAPI(t, catalog)

	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("derivative build mapping blockers = %#v", plan.Review)
	}
	if prepared.Instances[0].BuildSource == nil || prepared.Instances[0].BuildSource.ServiceName != "php" {
		t.Fatalf("build source = %#v", prepared.Instances[0].BuildSource)
	}
}

func TestPreflightTargetPreparesUniqueConnectBuildSource(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
	export.Apps[0].Instances[0].Properties = map[string]interface{}{
		"git_target_value": "source-fallback",
		"git_target_type":  "branch",
		"deployment_type":  "git",
	}
	options := preflightOwnerPlanOptions()
	options.Repository = RepositoryTargetPlan{
		GitIntegrationID: 55,
	}
	plan := preflightBuildPlan(t, export, options)
	catalog := preflightSingleBuildCatalog("php", false)
	api := newPreflightTargetAPI(t, catalog)

	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{
			SkipData:   true,
			GitRef:     " 0123456789abcdef ",
			GitRefType: " ShA ",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	build := prepared.Instances[0].BuildSource
	if build == nil ||
		build.ServiceName != "php" ||
		build.Input.BuildSourceType != TargetBuildSourceConnect ||
		build.Input.IntegrationID == nil ||
		*build.Input.IntegrationID != 55 ||
		build.Input.RemoteGitRepoID == nil ||
		*build.Input.RemoteGitRepoID != "remote-repo-29" ||
		build.Input.GitRef == nil ||
		*build.Input.GitRef != "0123456789abcdef" ||
		build.Input.GitRefType == nil ||
		*build.Input.GitRefType != TargetGitRefCommit {
		t.Fatalf("prepared build source = %#v", build)
	}
	if plan.Apps[0].Repository == nil || plan.Apps[0].Repository.TargetService != "php" {
		t.Fatalf("repository target service = %#v", plan.Apps[0].Repository)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("unique build source blockers = %#v", plan.Review)
	}
}

func TestPreflightTargetRejectsAmbiguousConnectBuildServicesUnlessExplicit(t *testing.T) {
	export := preflightFixtureExport(true)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "node", Enabled: true},
		{Name: "php", Enabled: true},
	}
	catalog := preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{
			71: {
				{ID: 11, Name: "php", ServiceRevID: 101},
				{ID: 14, Name: "node", ServiceRevID: 104},
			},
		},
		revisions: map[int]TargetServiceRevision{
			101: {
				ID: 101, ServiceID: 201, Name: "php",
				Manifest: &TargetServiceManifest{
					Name: "php", Build: &TargetServiceBuildCapability{Connect: true},
				},
			},
			104: {
				ID: 104, ServiceID: 204, Name: "node",
				Manifest: &TargetServiceManifest{
					Name: "node", Build: &TargetServiceBuildCapability{Connect: true},
				},
			},
		},
	}
	api := newPreflightTargetAPI(t, catalog)

	ambiguousOptions := preflightOwnerPlanOptions()
	ambiguousOptions.Repository = RepositoryTargetPlan{
		GitIntegrationID: 44,
	}
	ambiguousPlan := preflightBuildPlan(t, export, ambiguousOptions)
	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&ambiguousPlan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Instances[0].BuildSource != nil {
		t.Fatalf("ambiguous build source selected %#v", prepared.Instances[0].BuildSource)
	}
	if !preflightHasReview(
		ambiguousPlan,
		SeverityBlocking,
		"repository target service",
		"2 enabled connect-build services",
	) {
		t.Fatalf("ambiguous build review = %#v", ambiguousPlan.Review)
	}

	explicitOptions := ambiguousOptions
	explicitOptions.Repository.Service = "node"
	explicitPlan := preflightBuildPlan(t, export, explicitOptions)
	prepared, err = api.client.PreflightTarget(
		context.Background(),
		export,
		&explicitPlan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Instances[0].BuildSource == nil ||
		prepared.Instances[0].BuildSource.ServiceName != "node" {
		t.Fatalf("explicit build source = %#v", prepared.Instances[0].BuildSource)
	}
	if explicitPlan.Summary.Blocking != 0 {
		t.Fatalf("explicit build mapping blockers = %#v", explicitPlan.Review)
	}
}

func TestPreflightTargetBlocksMissingAndDisabledRequiredServices(t *testing.T) {
	tests := []struct {
		name          string
		sourceService Service
		targetService TargetStackService
		revision      TargetServiceRevision
		message       string
	}{
		{
			name:          "enabled source service missing",
			sourceService: Service{Name: "php", Enabled: true},
			targetService: TargetStackService{ID: 15, Name: "nginx", ServiceRevID: 105},
			revision: TargetServiceRevision{
				ID: 105, ServiceID: 205, Name: "nginx",
				Manifest: &TargetServiceManifest{Name: "nginx"},
			},
			message: `target stack has no service named "php"`,
		},
		{
			name:          "disabled source maps to required target",
			sourceService: Service{Name: "php", Enabled: false},
			targetService: TargetStackService{
				ID: 11, Name: "php", Required: true, ServiceRevID: 101,
			},
			revision: TargetServiceRevision{
				ID: 101, ServiceID: 201, Name: "php",
				Manifest: &TargetServiceManifest{Name: "php"},
			},
			message: `source service is disabled but target service "php" is required`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			export := preflightFixtureExport(false)
			export.Apps[0].Instances[0].Services = []Service{test.sourceService}
			plan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
			catalog := preflightTargetCatalog{
				stacks: map[string]TargetStack{
					"drupal11": {
						ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
					},
				},
				stackServices: map[int][]TargetStackService{71: {test.targetService}},
				revisions: map[int]TargetServiceRevision{
					test.revision.ID: test.revision,
				},
			}
			api := newPreflightTargetAPI(t, catalog)

			prepared, err := api.client.PreflightTarget(
				context.Background(),
				export,
				&plan,
				TargetPreflightOptions{SkipCode: true, SkipData: true},
			)
			if err != nil {
				t.Fatal(err)
			}
			if !preflightHasReview(plan, SeverityBlocking, "service php", test.message) {
				t.Fatalf("service review = %#v", plan.Review)
			}
			if plan.Status != "blocked" {
				t.Fatalf("plan status = %q", plan.Status)
			}
			if _, found := prepared.Instances[0].Services["php"]; found {
				t.Fatalf("blocked service was prepared: %#v", prepared.Instances[0].Services)
			}
		})
	}
}

func TestPreflightTargetBlocksDisabledInheritedEnvironmentOverride(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = []Service{{
		Name:    "php",
		Enabled: true,
		EnvVars: []EnvVar{{
			Name:           "INHERITED_DEFAULT",
			Enabled:        false,
			Origin:         "custom",
			OverrideFields: []string{"enabled"},
		}},
	}}
	options := preflightOwnerPlanOptions()
	options.SkipCode = true
	options.SkipData = true
	plan := preflightBuildPlan(t, export, options)
	api := newPreflightTargetAPI(t, preflightSingleBuildCatalog("php", false))

	if _, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	); err != nil {
		t.Fatal(err)
	}
	if !preflightHasReview(
		plan,
		SeverityBlocking,
		"env var INHERITED_DEFAULT",
		"disabled inherited environment-variable override cannot be represented safely",
	) {
		t.Fatalf("environment override review = %#v", plan.Review)
	}
}

func TestPreflightTargetResolvesUniqueImportsAndRequiresMapForAmbiguity(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = []Service{
		{Name: "php", Enabled: true},
		{Name: "mariadb", Enabled: true},
		{Name: "files-nfs", Enabled: true},
	}
	export.Apps[0].Instances[0].Backups = []Backup{
		{
			UUID: "backup-db", BackupUUID: "snapshot-db", Component: "db",
			URL: "https://backups.example.test/database.sql.gz", Status: "ok",
		},
		{
			UUID: "backup-files", BackupUUID: "snapshot-files", Component: "files",
			URL: "https://backups.example.test/files.tar.gz", Status: "ok",
		},
	}

	t.Run("unique database and files capabilities", func(t *testing.T) {
		plan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
		api := newPreflightTargetAPI(t, preflightDataCatalog(false))
		prepared, err := api.client.PreflightTarget(
			context.Background(),
			export,
			&plan,
			TargetPreflightOptions{SkipCode: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Summary.Blocking != 0 {
			t.Fatalf("unique import blockers = %#v", plan.Review)
		}
		assertPreflightImportPlan(t, &plan.Apps[0].Instances[0], "db", "mariadb", "database", 12)
		assertPreflightImportPlan(t, &plan.Apps[0].Instances[0], "files", "files-nfs", "files", 13)
		if hasReviewMessage(plan.Review, SeverityConfirmation, "backup will import") ||
			hasReviewMessage(plan.Review, SeverityConfirmation, "selected only when the target stack exposes") {
			t.Fatalf("deterministic database import should be shown in the migration table, not as a review item: %#v", plan.Review)
		}
		if len(prepared.Instances[0].Imports) != 2 {
			t.Fatalf("prepared imports = %#v", prepared.Instances[0].Imports)
		}
	})

	t.Run("ambiguous database capability", func(t *testing.T) {
		plan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
		api := newPreflightTargetAPI(t, preflightDataCatalog(true))
		prepared, err := api.client.PreflightTarget(
			context.Background(),
			export,
			&plan,
			TargetPreflightOptions{SkipCode: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if !preflightHasReview(
			plan,
			SeverityBlocking,
			"backup db",
			"matched 2 enabled target imports",
		) {
			t.Fatalf("ambiguous import review = %#v", plan.Review)
		}
		if _, found := prepared.Instances[0].Imports["backup-db"]; found {
			t.Fatalf("ambiguous database import was prepared: %#v", prepared.Instances[0].Imports)
		}
		if prepared.Instances[0].Imports["backup-files"].ServiceName != "files-nfs" {
			t.Fatalf("unique files import = %#v", prepared.Instances[0].Imports["backup-files"])
		}
	})

	t.Run("files default remains files-nfs when another files import exists", func(t *testing.T) {
		catalog := preflightDataCatalog(false)
		catalog.stackServices[71] = append(catalog.stackServices[71], TargetStackService{
			ID: 14, Name: "other-files", ServiceRevID: 104,
		})
		catalog.revisions[104] = TargetServiceRevision{
			ID: 104, Name: "other-files", ServiceID: 204,
			Manifest: &TargetServiceManifest{
				Name: "other-files",
				Imports: []TargetServiceImportCapability{
					{Name: "files", Volume: "data"},
				},
			},
		}
		plan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
		api := newPreflightTargetAPI(t, catalog)
		prepared, err := api.client.PreflightTarget(
			context.Background(),
			export,
			&plan,
			TargetPreflightOptions{SkipCode: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Summary.Blocking != 0 {
			t.Fatalf("files import blockers = %#v", plan.Review)
		}
		assertPreflightImportPlan(t, &plan.Apps[0].Instances[0], "files", "files-nfs", "files", 13)
		files := prepared.Instances[0].Imports["backup-files"]
		if files.ServiceName != "files-nfs" || files.ImportName != "files" {
			t.Fatalf("prepared files import = %#v", files)
		}
	})

	t.Run("explicit database import map", func(t *testing.T) {
		options := preflightOwnerPlanOptions()
		options.TargetImportMap = map[string]string{
			"db": "mariadb:database",
		}
		plan := preflightBuildPlan(t, export, options)
		api := newPreflightTargetAPI(t, preflightDataCatalog(true))
		prepared, err := api.client.PreflightTarget(
			context.Background(),
			export,
			&plan,
			TargetPreflightOptions{SkipCode: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Summary.Blocking != 0 {
			t.Fatalf("explicit import mapping blockers = %#v", plan.Review)
		}
		assertPreflightImportPlan(t, &plan.Apps[0].Instances[0], "db", "mariadb", "database", 12)
		if hasReviewMessage(plan.Review, SeverityConfirmation, "backup will import") ||
			hasReviewMessage(plan.Review, SeverityConfirmation, "backup will use explicit target import") {
			t.Fatalf("explicit database import should be shown in the migration table, not as a review item: %#v", plan.Review)
		}
		database := prepared.Instances[0].Imports["backup-db"]
		if database.ServiceName != "mariadb" || database.ImportName != "database" {
			t.Fatalf("explicit database import = %#v", database)
		}
	})
}

func TestPreflightTargetUsesCustomCIForBuildServiceWithoutSourceRepository(t *testing.T) {
	export := preflightFixtureExport(false)
	export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
	api := newPreflightTargetAPI(t, preflightSingleBuildCatalog("php", false))

	plan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
	prepared, err := api.client.PreflightTarget(
		context.Background(),
		export,
		&plan,
		TargetPreflightOptions{SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Instances[0].BuildSource == nil || prepared.Instances[0].BuildSource.ServiceName != "php" ||
		prepared.Instances[0].BuildSource.Input.BuildSourceType != "" || !prepared.Instances[0].ExternalCIOnly {
		t.Fatalf("build source without repository = %#v", prepared.Instances[0])
	}
	if !preflightHasReview(
		plan,
		SeverityMigration,
		"application code",
		"Custom CI without a linked Git repository",
	) {
		t.Fatalf("missing repository review = %#v", plan.Review)
	}
	if plan.Summary.Blocking != 0 {
		t.Fatalf("repository-less Custom CI migration blocked: %#v", plan.Review)
	}

	skippedPlan := preflightBuildPlan(t, export, preflightOwnerPlanOptions())
	prepared, err = api.client.PreflightTarget(
		context.Background(),
		export,
		&skippedPlan,
		TargetPreflightOptions{SkipCode: true, SkipData: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Instances[0].BuildSource != nil || skippedPlan.Summary.Blocking != 0 {
		t.Fatalf("intentional code skip result: build=%#v review=%#v", prepared.Instances[0].BuildSource, skippedPlan.Review)
	}
}

func TestNormalizeGitRefType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "branch", want: TargetGitRefBranch},
		{input: " BRANCH ", want: TargetGitRefBranch},
		{input: "tag", want: TargetGitRefTag},
		{input: "TaG", want: TargetGitRefTag},
		{input: "commit", want: TargetGitRefCommit},
		{input: " sha ", want: TargetGitRefCommit},
		{input: "", want: ""},
		{input: "ref", want: ""},
		{input: "refs/heads/main", want: ""},
	}
	for _, test := range tests {
		t.Run(strings.ReplaceAll(test.input, " ", "_"), func(t *testing.T) {
			if got := normalizeGitRefType(test.input); got != test.want {
				t.Fatalf("normalizeGitRefType(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

type preflightTargetCatalog struct {
	apps               []TargetApp
	publicStacks       []TargetStack
	remoteGitRepos     map[int][]TargetRemoteGitRepo
	remoteGitRepoFiles map[string]bool
	stacks             map[string]TargetStack
	stackRevisions     map[int]TargetStackRevision
	stackServices      map[int][]TargetStackService
	revisions          map[int]TargetServiceRevision
}

type preflightTargetAPI struct {
	client *TargetClient
	mu     sync.Mutex
	paths  []string
}

func newPreflightTargetAPI(t *testing.T, catalog preflightTargetCatalog) *preflightTargetAPI {
	t.Helper()
	api := &preflightTargetAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		api.mu.Lock()
		api.paths = append(api.paths, request.URL.Path)
		api.mu.Unlock()

		if request.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch {
		case request.URL.Path == "/v1/providers/by-name/custom-ci":
			preflightWriteJSON(w, TargetProvider{ID: 501, RevID: 502, Name: "custom-ci", Title: "Custom CI"})
		case request.URL.Path == "/v1/catalog/stacks":
			preflightWriteJSON(w, catalog.publicStacks)
		case request.URL.Path == "/v1/apps":
			preflightWriteJSON(w, catalog.apps)
		case strings.HasPrefix(request.URL.Path, "/v1/integrations/") &&
			strings.HasSuffix(request.URL.Path, "/options/remote-git-repo-file"):
			value := strings.TrimSuffix(
				strings.TrimPrefix(request.URL.Path, "/v1/integrations/"),
				"/options/remote-git-repo-file",
			)
			integrationID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid integration ID", http.StatusBadRequest)
				return
			}
			query := request.URL.Query()
			key := preflightRemoteGitRepoFileKey(
				integrationID,
				query.Get("remoteGitRepoId"),
				query.Get("path"),
				query.Get("ref"),
			)
			exists := true
			if configured, found := catalog.remoteGitRepoFiles[key]; found {
				exists = configured
			}
			preflightWriteJSON(w, map[string]bool{"exists": exists})
		case strings.HasPrefix(request.URL.Path, "/v1/integrations/") &&
			strings.HasSuffix(request.URL.Path, "/options/remote-git-repos"):
			value := strings.TrimSuffix(
				strings.TrimPrefix(request.URL.Path, "/v1/integrations/"),
				"/options/remote-git-repos",
			)
			integrationID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid integration ID", http.StatusBadRequest)
				return
			}
			if catalog.remoteGitRepos != nil {
				preflightWriteJSON(w, catalog.remoteGitRepos[integrationID])
				return
			}
			repositories := []TargetRemoteGitRepo{}
			switch integrationID {
			case 44:
				repositories = append(repositories, TargetRemoteGitRepo{ID: "remote-repo-17", Name: "acme/example"})
			case 55:
				repositories = append(repositories, TargetRemoteGitRepo{ID: "remote-repo-29", Name: "acme/example"})
			}
			preflightWriteJSON(w, repositories)
		case request.URL.Path == "/v1/stacks":
			if request.URL.Query().Get("orgId") != "8" ||
				request.URL.Query().Get("projectIds") != "9" {
				http.Error(w, "invalid stack scope", http.StatusBadRequest)
				return
			}
			name := request.URL.Query().Get("search")
			stack, found := catalog.stacks[name]
			if !found {
				preflightWriteJSON(w, TargetStacksResponse{Items: []TargetStack{}})
				break
			}
			if stack.Status == "" {
				stack.Status = "OK"
			}
			preflightWriteJSON(w, TargetStacksResponse{
				Items:      []TargetStack{stack},
				TotalCount: 1,
			})
		case strings.HasPrefix(request.URL.Path, "/v1/stacks/"):
			value := strings.TrimPrefix(request.URL.Path, "/v1/stacks/")
			stackID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid stack ID", http.StatusBadRequest)
				return
			}
			for _, stack := range catalog.stacks {
				if stack.ID == stackID {
					if stack.Status == "" {
						stack.Status = "OK"
					}
					preflightWriteJSON(w, stack)
					return
				}
			}
			http.NotFound(w, request)
		case strings.HasPrefix(request.URL.Path, "/v1/stack-revisions/") &&
			strings.HasSuffix(request.URL.Path, "/services"):
			value := strings.TrimSuffix(
				strings.TrimPrefix(request.URL.Path, "/v1/stack-revisions/"),
				"/services",
			)
			revisionID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid revision ID", http.StatusBadRequest)
				return
			}
			services, found := catalog.stackServices[revisionID]
			if !found {
				http.NotFound(w, request)
				return
			}
			preflightWriteJSON(w, services)
		case strings.HasPrefix(request.URL.Path, "/v1/stack-revisions/"):
			value := strings.TrimPrefix(request.URL.Path, "/v1/stack-revisions/")
			revisionID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid revision ID", http.StatusBadRequest)
				return
			}
			revision, found := catalog.stackRevisions[revisionID]
			if !found {
				for _, stack := range catalog.stacks {
					if stack.RevID == revisionID {
						revision = TargetStackRevision{
							ID: revisionID, Name: stack.Name, Number: stack.LatestRevNumber, StackID: stack.ID,
						}
						found = true
						break
					}
				}
				if !found {
					http.NotFound(w, request)
					return
				}
			}
			preflightWriteJSON(w, revision)
		case strings.HasPrefix(request.URL.Path, "/v1/service-revisions/"):
			value := strings.TrimPrefix(request.URL.Path, "/v1/service-revisions/")
			revisionID, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, "invalid service revision ID", http.StatusBadRequest)
				return
			}
			revision, found := catalog.revisions[revisionID]
			if !found {
				http.NotFound(w, request)
				return
			}
			if revision.Manifest != nil && len(revision.Manifest.Settings) == 0 && strings.Contains(revision.Name, "php") {
				revision.Manifest.Settings = drupalPHPSettingManifest().Settings
			}
			preflightWriteJSON(w, revision)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	api.client = client
	return api
}

func preflightRemoteGitRepoFileKey(integrationID int, remoteGitRepoID, filePath, ref string) string {
	return strings.Join([]string{strconv.Itoa(integrationID), remoteGitRepoID, filePath, ref}, "\x00")
}

func (a *preflightTargetAPI) requestPaths() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.paths...)
}

func preflightWriteJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func preflightOfficialCatalog() preflightTargetCatalog {
	originName := "drupal11"
	return preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "acme/drupal11", Title: "Drupal 11", Status: "OK",
				RevID: 71, LatestRevNumber: 4, OriginStackRevName: &originName, OrgID: 8,
			},
		},
		stackRevisions: map[int]TargetStackRevision{
			71: {ID: 71, Name: "drupal11", Number: 4, Version: "11", StackID: 7},
		},
		stackServices: map[int][]TargetStackService{
			71: {
				{
					ID: 11, Name: "php", Title: "PHP", Type: "backend",
					Main: true, Required: true, Replicas: 1, ServiceRevID: 101,
				},
				{
					ID: 13, Name: "files-nfs", Title: "Files", Type: "storage",
					Required: true, Replicas: 1, ServiceRevID: 103,
				},
				{
					ID: 12, Name: "mariadb", Title: "MariaDB", Type: "database",
					Required: true, Replicas: 1, ServiceRevID: 102,
				},
			},
		},
		revisions: map[int]TargetServiceRevision{
			101: {
				ID: 101, Name: "php", ServiceID: 201, Number: 4, Version: "8.3",
				Manifest: &TargetServiceManifest{
					Name: "php", Build: &TargetServiceBuildCapability{Connect: true},
				},
			},
			102: {
				ID: 102, Name: "mariadb", ServiceID: 202, Number: 3, Version: "11",
				Manifest: &TargetServiceManifest{
					Name: "mariadb",
					Imports: []TargetServiceImportCapability{
						{Name: "database", Volume: "data", Extensions: []string{"sql.gz"}},
					},
				},
			},
			103: {
				ID: 103, Name: "files-nfs", ServiceID: 203, Number: 2,
				Manifest: &TargetServiceManifest{
					Name: "files-nfs",
					Imports: []TargetServiceImportCapability{
						{Name: "files", Volume: "files", Extensions: []string{"tar.gz"}},
					},
				},
			},
		},
	}
}

func preflightSingleBuildCatalog(name string, disabled bool) preflightTargetCatalog {
	return preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{
			71: {
				{ID: 11, Name: name, Disabled: disabled, ServiceRevID: 101},
			},
		},
		revisions: map[int]TargetServiceRevision{
			101: {
				ID: 101, Name: name, ServiceID: 201,
				Manifest: &TargetServiceManifest{
					Name: name, Build: &TargetServiceBuildCapability{Connect: true},
				},
			},
		},
	}
}

func preflightDataCatalog(ambiguousDatabase bool) preflightTargetCatalog {
	services := []TargetStackService{
		{ID: 11, Name: "php", ServiceRevID: 101},
		{ID: 12, Name: "mariadb", ServiceRevID: 102},
		{ID: 13, Name: "files-nfs", ServiceRevID: 103},
	}
	revisions := map[int]TargetServiceRevision{
		101: {
			ID: 101, Name: "drupal11-php", ServiceID: 201,
			Manifest: &TargetServiceManifest{Name: "drupal11-php"},
		},
		102: {
			ID: 102, Name: "mariadb", ServiceID: 202,
			Manifest: &TargetServiceManifest{
				Name: "mariadb",
				Imports: []TargetServiceImportCapability{
					{Name: "database", Volume: "data"},
				},
			},
		},
		103: {
			ID: 103, Name: "files-nfs", ServiceID: 203,
			Manifest: &TargetServiceManifest{
				Name: "files-nfs",
				Imports: []TargetServiceImportCapability{
					{Name: "files", Volume: "files"},
				},
			},
		},
	}
	if ambiguousDatabase {
		services = append(services, TargetStackService{
			ID: 14, Name: "mysql", ServiceRevID: 104,
		})
		revisions[104] = TargetServiceRevision{
			ID: 104, Name: "mysql", ServiceID: 204,
			Manifest: &TargetServiceManifest{
				Name: "mysql",
				Imports: []TargetServiceImportCapability{
					{Name: "database", Volume: "data"},
				},
			},
		}
	}
	return preflightTargetCatalog{
		stacks: map[string]TargetStack{
			"drupal11": {
				ID: 7, Name: "drupal11", RevID: 71, LatestRevNumber: 4, OrgID: 8,
			},
		},
		stackServices: map[int][]TargetStackService{71: services},
		revisions:     revisions,
	}
}

func preflightFixtureExport(withRepository bool) Export {
	docroot := "web"
	siteName := "default"
	app := App{
		UUID: "app-1", Name: "example", Title: "Example", Type: "app",
		Docroot: &docroot, SiteName: &siteName, Status: "ok", Created: 10, Updated: 20,
	}
	if withRepository {
		app.Repository = &Repository{
			UUID: "repo-1", Title: "Example repository",
			URL: "https://git.example.test/acme/example.git", Status: "ok",
		}
	}
	return Export{
		Schema:          ExportSchemaV2,
		GeneratedAt:     100,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []AppExport{
			{
				App: app,
				Instances: []Instance{
					{
						UUID: "inst-1", Name: "prod", Title: "Production",
						Type: "prod", Status: "ok", Updated: 30,
						Stack: Stack{UUID: "stack-1", Name: "drupal11", Version: "11"},
						Properties: map[string]interface{}{
							"git_target_value": "main",
							"git_target_type":  "branch",
							"deployment_type":  "git",
						},
					},
				},
			},
		},
	}
}

func preflightOwnerPlanOptions() PlanOptions {
	userID := 42
	return PlanOptions{
		SourceKind:    "app",
		SourceID:      "app-1",
		TargetOrg:     "acme",
		TargetProject: "customer",
		TargetCluster: "production",
		TargetStackID: 7,
		TargetScope: &TargetScopeDiscovery{
			User: TargetCurrentUser{
				ID: userID, Email: "owner@example.test", IsAdmin: false,
			},
			Membership: TargetOrgMembership{
				ID: 88, UserID: &userID, OrgID: 8, Role: "owner", Status: "ok",
			},
			Org: TargetOrg{
				ID: 8, Name: "acme", Title: "Acme",
				Capabilities: &TargetOrgCapabilities{CustomDomains: true, CronSchedules: true},
				Subscription: &TargetOrgSubscription{
					Status: "ACTIVE",
					Plan: &TargetOrgSubscriptionPlan{
						Name: "team", Title: "Team", Usage: 4, UsageIncluded: 10,
					},
				},
			},
			Project: TargetProject{ID: 9, Name: "customer", Title: "Customer", OrgID: 8},
			Cluster: TargetCluster{
				ID: 10, Name: "production", Title: "Production", Status: "OK", OrgID: 8,
				Capabilities: TargetClusterCapabilities{
					EnvoyGateway: true, RedirectRoutes: true,
				},
			},
		},
		TargetEnvs: map[string]TargetEnv{
			"prod": {
				ID: 11, Name: "prod", Title: "Production", Type: "PROD", OrgID: 8,
			},
		},
	}
}

func preflightBuildPlan(t *testing.T, export Export, options PlanOptions) Plan {
	t.Helper()
	plan, err := BuildPlan(export, options)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func preflightClonePlan(t *testing.T, source Plan) Plan {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var cloned Plan
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func assertPreflightServicePlan(
	t *testing.T,
	instance *InstancePlan,
	sourceName string,
	targetName string,
	targetID int,
	serviceRevisionID int,
) {
	t.Helper()
	service := preflightFindServicePlan(t, instance, sourceName)
	if service.TargetName != targetName ||
		service.TargetID != targetID ||
		service.TargetServiceRevID != serviceRevisionID {
		t.Fatalf("service %q plan = %#v", sourceName, service)
	}
}

func preflightFindServicePlan(t *testing.T, instance *InstancePlan, sourceName string) *ServicePlan {
	t.Helper()
	for index := range instance.Services {
		if instance.Services[index].SourceName == sourceName {
			return &instance.Services[index]
		}
	}
	t.Fatalf("service plan %q not found in %#v", sourceName, instance.Services)
	return nil
}

func assertPreflightImportPlan(
	t *testing.T,
	instance *InstancePlan,
	component string,
	service string,
	importName string,
	serviceID int,
) {
	t.Helper()
	for index := range instance.Imports {
		item := &instance.Imports[index]
		if item.Component != component {
			continue
		}
		if item.TargetService != service ||
			item.TargetImport != importName ||
			item.TargetServiceID != serviceID ||
			item.TargetServiceRevID <= 0 {
			t.Fatalf("import %q plan = %#v", component, item)
		}
		return
	}
	t.Fatalf("import plan %q not found in %#v", component, instance.Imports)
}

func preflightHasReview(plan Plan, severity string, subject string, message string) bool {
	for _, item := range plan.Review {
		if item.Severity == severity &&
			strings.Contains(strings.ToLower(item.Subject), strings.ToLower(subject)) &&
			strings.Contains(strings.ToLower(item.Message), strings.ToLower(message)) {
			return true
		}
	}
	return false
}

func equalPreflightStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
