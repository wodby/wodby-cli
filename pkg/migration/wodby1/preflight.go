package wodby1

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

const wodbyCIPipelinePath = ".wodby/pipeline.yml"

// TargetPreflightOptions contains the explicit customer choices that affect
// target-side migration preparation. Mapping selectors themselves are already
// recorded in Plan.
type TargetPreflightOptions struct {
	SkipCode                    bool
	SkipData                    bool
	GitRef                      string
	GitRefType                  string
	CodeService                 string
	AllowedTargetAppID          int
	AllowStateBackedAppRecovery bool
	AllowedTargetAppIDs         map[string]int
	StateBackedAppRecovery      map[string]bool
	AllowedTargetInstanceIDs    map[string]int
	StateBackedInstanceRecovery map[string]bool
	AddMissingServices          bool
	Progress                    func(TargetPreflightProgress)
}

// TargetPreflightProgress reports the current app and, when applicable,
// instance while target-side migration mappings are inspected. Indexes are
// one-based so callers can render them directly.
type TargetPreflightProgress struct {
	Stage         string
	AppIndex      int
	AppTotal      int
	AppName       string
	InstanceIndex int
	InstanceTotal int
	InstanceName  string
}

// PreparedMigration is an in-memory target mapping. It can contain protected
// values needed for stack variables or integrations, so it is rebuilt from the
// source export and approved plan on every run and is never persisted in the
// plan or state file. Mutation phases pin and read back immutable target IDs.
type PreparedMigration struct {
	App                AppExport
	Instances          []PreparedInstance
	Apps               []PreparedAppMigration
	StackConfiguration PreparedStackConfiguration
	StackAdditions     []PreparedStackServiceAddition
	Integrations       []PreparedIntegration
}

type PreparedAppMigration struct {
	App                AppExport
	Instances          []PreparedInstance
	StackConfiguration PreparedStackConfiguration
	StackAdditions     []PreparedStackServiceAddition
	Integrations       []PreparedIntegration
}

// ForApp returns the single-app target mapping consumed by MigrationExecutor.
func (p PreparedMigration) ForApp(sourceUUID string) (PreparedMigration, bool) {
	for _, app := range p.Apps {
		if app.App.App.UUID == sourceUUID {
			return PreparedMigration{App: app.App, Instances: app.Instances, Apps: []PreparedAppMigration{app}, StackConfiguration: app.StackConfiguration, StackAdditions: app.StackAdditions, Integrations: app.Integrations}, true
		}
	}
	if p.App.App.UUID == sourceUUID {
		return PreparedMigration{App: p.App, Instances: p.Instances, StackConfiguration: p.StackConfiguration, StackAdditions: p.StackAdditions, Integrations: p.Integrations}, true
	}
	return PreparedMigration{}, false
}

type PreparedInstance struct {
	Source               Instance
	SkipCode             bool
	Stack                TargetStack
	StackServices        []TargetStackServiceInspection
	Services             map[string]PreparedService
	BuildSource          *PreparedBuildSource
	Imports              map[string]PreparedImport
	ImportByComponent    map[string]PreparedImport
	EffectiveState       map[string]bool
	DisableCronSchedules bool
	DisableCustomRoutes  bool
	TargetEnvType        string
	StackAdditions       []PreparedStackServiceAddition
	BackupDestination    *PreparedBackupDestination
	CIIntegrationKey     string
	CIIntegrationID      int
	UsesWodbyCI          bool
	ExternalCIOnly       bool
	ExternalCI           *PreparedExternalCI
	ServiceLinks         []PreparedAppServiceLink
}

// PreparedExternalCI carries the third-party CI facts resolved while planning
// so the executor can explain the manual bootstrap build without re-deriving
// them from the source export.
type PreparedExternalCI struct {
	// ProviderKey and ProviderLabel come from the CI provider Wodby 1 recorded
	// on the instance's last successful build. Both are empty when Wodby 1
	// reported nothing recognizable.
	ProviderKey   string
	ProviderLabel string
	// ProviderSupported reports whether Wodby 2 has a CI provider for it.
	// Wodby 1 recognizes providers Wodby 2 does not support, and those
	// operators must adapt one of the supported examples instead.
	ProviderSupported bool
	// ExampleURL is the closest wodby/wodby-ci 2.0 page for this app.
	ExampleURL string
}

// PreparedStackConfiguration is the application-wide configuration applied to
// the target stack before any app instance is created. Service state and build
// refs remain instance-level because Wodby 2 models them on app services.
type PreparedStackConfiguration struct {
	EnvVars  []PreparedStackEnvVar
	Services map[string]PreparedStackServiceConfiguration
}

type PreparedStackServiceAddition struct {
	Name              string
	Title             string
	ServiceID         int
	ServiceRevisionID int
	Inspection        TargetStackServiceInspection
}

type PreparedStackServiceConfiguration struct {
	Replicas        *int
	Resources       *PreparedServiceResources
	VersionOptions  []TargetStackServiceOptionInput
	EnvVars         []PreparedStackEnvVar
	Settings        map[string]string
	SettingMappings []PreparedStackSettingMapping
	CronSchedules   []PreparedStackCronSchedule
	Integrations    []PreparedStackIntegrationLink
	Links           []PreparedStackServiceLink
}

// PreparedStackSettingMapping describes a source-to-target setting conversion
// for the human-readable migration review. It is deliberately kept in the
// in-memory prepared migration and is not persisted in the migration plan.
type PreparedStackSettingMapping struct {
	Source string
	Name   string
	Value  string
	Action string
}

type PreparedStackServiceLink struct {
	Name              string
	LinkedServiceName string
}

type PreparedAppServiceLink struct {
	ServiceName       string
	Name              string
	LinkedServiceName string
}

type PreparedIntegration struct {
	Key              string
	ProviderName     string
	ProviderID       int
	ProviderRevID    int
	Name             string
	Title            string
	Kind             string
	Service          string
	Scope            *string
	Fields           []TargetIntegrationFieldInput
	TargetID         int
	VariableProvider *PreparedVariableProvider
}

type PreparedVariableProvider struct {
	Name   string
	Title  string
	Fields []TargetVariableProviderFieldInput
}

type PreparedStackIntegrationLink struct {
	Name           string
	IntegrationKey string
	IntegrationID  int
}

type PreparedBackupDestination struct {
	IntegrationKey string
	IntegrationID  int
	Bucket         string
	Auto           bool
	Disabled       bool
	TimeZone       string
}

type PreparedStackEnvVar struct {
	Name    string
	Value   string
	Secret  bool
	EnvType *string
}

type PreparedStackCronSchedule struct {
	Name     string
	Title    string
	Crontab  string
	Command  string
	Workload *string
	Disabled bool
	EnvType  *string
}

type PreparedService struct {
	Source           Service
	Target           TargetStackServiceInspection
	TargetVersion    string
	InstanceVersion  string
	InstanceEnvVars  []EnvVar
	InstanceCronJobs []CronJob
	Replicas         *int
	Resources        *PreparedServiceResources
}

type PreparedServiceResources struct {
	Workload   string
	Container  string
	RequestCPU *int
	RequestMem *int
	LimitCPU   *int
	LimitMem   *int
}

type PreparedBuildSource struct {
	ServiceName string
	Input       TargetBuildSourceInput
}

type PreparedImport struct {
	Source       Backup
	ServiceName  string
	ImportName   string
	StackService TargetStackServiceInspection
}

// PreflightTarget resolves immutable stack revisions and validates every
// service, repository, and import mapping against the selected Wodby 2 target.
// It performs reads only and folds its findings into plan before calculating
// the approval hash.
func (c *TargetClient) PreflightTarget(
	ctx context.Context,
	export Export,
	plan *Plan,
	opts TargetPreflightOptions,
) (PreparedMigration, error) {
	if c == nil {
		return PreparedMigration{}, errors.New("target Wodby 2 client is required")
	}
	if plan == nil {
		return PreparedMigration{}, errors.New("migration plan is required")
	}
	if err := export.ValidateSource(plan.Source.Kind, plan.Source.ID); err != nil {
		return PreparedMigration{}, err
	}
	appExports := append([]AppExport(nil), export.AppExports()...)
	if (plan.Source.Kind == "app" || plan.Source.Kind == "instance") &&
		(len(appExports) != 1 || len(plan.Apps) != 1) {
		return PreparedMigration{}, errors.New("app or instance migration requires exactly one source app")
	}
	if plan.Source.Kind == "instance" &&
		(len(appExports[0].Instances) != 1 || appExports[0].Instances[0].UUID != plan.Source.ID) {
		return PreparedMigration{}, errors.New("instance migration requires exactly the requested source instance")
	}
	if len(appExports) != len(plan.Apps) {
		return PreparedMigration{}, errors.New("migration plan app set does not match the source export")
	}
	if !plan.Target.DiscoveryVerified || !plan.Target.OrgOwnerOrAdminVerified {
		return PreparedMigration{}, errors.New("target organization owner/admin discovery is required before preflight")
	}

	sort.SliceStable(appExports, func(i, j int) bool {
		return compareApp(appExports[i].App, appExports[j].App) < 0
	})
	planApps := make(map[string]*AppPlan, len(plan.Apps))
	for index := range plan.Apps {
		item := &plan.Apps[index]
		if item.SourceUUID == "" || planApps[item.SourceUUID] != nil {
			return PreparedMigration{}, errors.New("migration plan contains an invalid source app set")
		}
		planApps[item.SourceUUID] = item
	}
	prepared := PreparedMigration{Apps: []PreparedAppMigration{}}
	findings := []ReviewItem{}
	for appIndex, appExport := range appExports {
		reportTargetPreflightProgress(opts.Progress, TargetPreflightProgress{
			Stage: "app", AppIndex: appIndex + 1, AppTotal: len(appExports), AppName: appExport.App.Name,
			InstanceTotal: len(appExport.Instances),
		})
		appPlan := planApps[appExport.App.UUID]
		if appPlan == nil {
			return PreparedMigration{}, errors.Errorf("migration plan is missing source app %q", appExport.App.UUID)
		}
		var existingApp TargetApp
		var appFound bool
		var err error
		if plan.Target.AppID > 0 {
			existingApp, appFound, err = c.FindAppByID(ctx, plan.Target.AppID)
		} else {
			existingApp, appFound, err = c.FindAppExact(ctx, plan.Target.OrgID, appExport.App.Name)
		}
		if err != nil {
			return PreparedMigration{}, errors.Wrap(err, "check target app availability")
		}
		allowedTargetAppID := opts.AllowedTargetAppID
		allowRecovery := opts.AllowStateBackedAppRecovery
		if plan.Source.Kind == "server" {
			allowedTargetAppID = opts.AllowedTargetAppIDs[appExport.App.UUID]
			allowRecovery = opts.StateBackedAppRecovery[appExport.App.UUID]
		}
		if plan.Target.AppID > 0 {
			if !appFound || existingApp.ID != plan.Target.AppID || existingApp.OrgID != plan.Target.OrgID || existingApp.Name != plan.Target.AppName {
				findings = append(findings, ReviewItem{
					Severity: SeverityBlocking, App: appExport.App.Name, Subject: "target app",
					Message: "the explicitly selected Wodby 2 target app no longer matches the reviewed organization and app identity",
				})
			} else {
				targetInstances, listErr := c.ListAppInstances(ctx, plan.Target.OrgID, existingApp.ID)
				if listErr != nil {
					return PreparedMigration{}, errors.Wrap(listErr, "inspect selected target app instances")
				}
				for _, sourceInstance := range appExport.Instances {
					for _, targetInstance := range targetInstances {
						if targetInstance.Name != sourceInstance.Name {
							continue
						}
						if opts.AllowedTargetInstanceIDs[sourceInstance.UUID] == targetInstance.ID {
							continue
						}
						if opts.StateBackedInstanceRecovery[sourceInstance.UUID] {
							continue
						}
						findings = append(findings, ReviewItem{
							Severity: SeverityBlocking, App: appExport.App.Name, Instance: sourceInstance.Name,
							Subject: "target instance name",
							Message: fmt.Sprintf("selected target app %q (ID %d) already contains instance %q (ID %d); the migration will not overwrite or adopt it", existingApp.Name, existingApp.ID, targetInstance.Name, targetInstance.ID),
						})
					}
				}
				findings = append(findings, ReviewItem{
					Severity: SeverityMigration, App: appExport.App.Name, Subject: "target app",
					Message: fmt.Sprintf("existing Wodby 2 app %q (ID %d) will be reused; only the planned new app instance will be created", existingApp.Name, existingApp.ID),
				})
			}
		} else if appFound && existingApp.ID != allowedTargetAppID && !allowRecovery {
			instanceNames := make([]string, 0, len(appExport.Instances))
			for _, instance := range appExport.Instances {
				instanceNames = append(instanceNames, fmt.Sprintf("%q", instance.Name))
			}
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      appExport.App.Name,
				Subject:  "target app name",
				Message: fmt.Sprintf(
					"target organization already contains app %q with ID %d; this blocks creation of planned target instance(s) %s. The migration will not overwrite or adopt the existing app or any of its instances. Remove or rename the unrelated target app, or resume with the original migration state file if that state created it",
					existingApp.Name,
					existingApp.ID,
					strings.Join(instanceNames, ", "),
				),
			})
		}
		repositoryFindings, err := c.resolveRepositoryPlan(ctx, appExport.App, appPlan.Repository, opts.SkipCode)
		if err != nil {
			return PreparedMigration{}, err
		}
		findings = append(findings, repositoryFindings...)

		planInstances := make(map[string]*InstancePlan, len(appPlan.Instances))
		for index := range appPlan.Instances {
			item := &appPlan.Instances[index]
			if item.SourceUUID == "" || planInstances[item.SourceUUID] != nil {
				return PreparedMigration{}, errors.Errorf("migration plan for app %q contains an invalid source instance set", appExport.App.UUID)
			}
			planInstances[item.SourceUUID] = item
		}
		preparedApp := PreparedAppMigration{App: appExport, Instances: []PreparedInstance{}}
		for instanceIndex, sourceInstance := range appExport.Instances {
			reportTargetPreflightProgress(opts.Progress, TargetPreflightProgress{
				Stage: "instance", AppIndex: appIndex + 1, AppTotal: len(appExports), AppName: appExport.App.Name,
				InstanceIndex: instanceIndex + 1, InstanceTotal: len(appExport.Instances), InstanceName: sourceInstance.Name,
			})
			instancePlan, found := planInstances[sourceInstance.UUID]
			if !found {
				return PreparedMigration{}, errors.Errorf("migration plan is missing source instance %q", sourceInstance.UUID)
			}
			preparedInstance, instanceFindings, err := c.preflightInstance(
				ctx,
				appExport.App,
				sourceInstance,
				instancePlan,
				plan.Target.OrgID,
				plan.Target.ProjectID,
				appPlan.Repository,
				opts,
				plan.Target.OrgCapabilities != nil && !plan.Target.OrgCapabilities.CronSchedules,
				plan.Target.OrgCapabilities != nil && !plan.Target.OrgCapabilities.CustomDomains,
			)
			if err != nil {
				return PreparedMigration{}, err
			}
			preparedApp.Instances = append(preparedApp.Instances, preparedInstance)
			findings = append(findings, instanceFindings...)
		}
		if len(preparedApp.Instances) != len(planInstances) {
			return PreparedMigration{}, errors.Errorf("migration plan instance set does not match source app %q", appExport.App.UUID)
		}
		stackAdditions, additionFindings := mergePreparedStackAdditions(appExport.App.Name, preparedApp.Instances)
		preparedApp.StackAdditions = stackAdditions
		findings = append(findings, additionFindings...)
		sort.SliceStable(preparedApp.Instances, func(i, j int) bool {
			return compareInstance(preparedApp.Instances[i].Source, preparedApp.Instances[j].Source) < 0
		})
		// Only an app- or server-scoped migration exports every instance of the
		// app; an instance-scoped one sees a single instance and must not treat
		// its values as app-wide.
		wholeApp := plan.Source.Kind != "instance"
		stackConfiguration, stackFindings, err := prepareStackConfiguration(&preparedApp, wholeApp)
		if err != nil {
			return PreparedMigration{}, err
		}
		if !wholeApp {
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration,
				App:      appExport.App.Name,
				Subject:  "instance-scoped configuration",
				Message: "only this instance is being migrated, so its configuration is applied to the target" +
					" stack scoped to its environment type rather than to every environment;" +
					" migrate the app's other instances later into this same app with --target-app",
			})
		}
		promoteSharedServiceCapacity(&preparedApp, &stackConfiguration)
		preparedApp.StackConfiguration = stackConfiguration
		findings = append(findings, stackFindings...)
		mailFindings := prepareMailDeliveryLinks(&preparedApp)
		findings = append(findings, mailFindings...)
		integrationFindings, err := c.prepareAppIntegrations(ctx, &preparedApp, appPlan, plan.Target)
		if err != nil {
			return PreparedMigration{}, err
		}
		findings = append(findings, integrationFindings...)
		if !opts.SkipCode {
			pipelineFindings, err := c.preflightWodbyCIPipelines(ctx, preparedApp, appPlan.Repository)
			if err != nil {
				return PreparedMigration{}, err
			}
			findings = append(findings, pipelineFindings...)
		}
		if appUsesExplicitTargetStack(appPlan) && stackConfigurationHasChanges(preparedApp.StackConfiguration) {
			findings = append(findings, ReviewItem{
				Severity: SeverityConfirmation,
				App:      appExport.App.Name,
				Subject:  "existing target stack configuration",
				Message:  "the selected existing target stack will receive a new published revision containing migrated replicas, resources, versions, variables, settings, schedules, and service links; existing app instances remain pinned to their current revisions and are not changed automatically",
			})
		}
		prepared.Apps = append(prepared.Apps, preparedApp)
		reportTargetPreflightProgress(opts.Progress, TargetPreflightProgress{
			Stage: "app_complete", AppIndex: appIndex + 1, AppTotal: len(appExports), AppName: appExport.App.Name,
		})
	}
	sharedVariableFindings, err := prepareSharedVariableIntegrations(&prepared, plan)
	if err != nil {
		return PreparedMigration{}, err
	}
	findings = append(findings, sharedVariableFindings...)
	if preparedMigrationUsesLegacyWodby1EnvVars(prepared) {
		findings = append(findings, ReviewItem{
			Severity: SeverityConfirmation,
			Subject:  "Wodby 1 environment compatibility",
			Message:  wodby1LegacyEnvVarsMarker + " will be enabled once at the migrated stack level. Wodby 2 will add supported legacy Wodby 1 runtime variable aliases to all app services; remove the marker after application code and commands use the native Wodby 2 variables",
		})
	}
	if len(prepared.Apps) == 1 {
		prepared.App = prepared.Apps[0].App
		prepared.Instances = prepared.Apps[0].Instances
		prepared.StackConfiguration = prepared.Apps[0].StackConfiguration
		prepared.StackAdditions = prepared.Apps[0].StackAdditions
		prepared.Integrations = prepared.Apps[0].Integrations
	}
	findings = append(findings, targetServiceCapacityFindings(plan, prepared, opts)...)
	if err := plan.AddReviewItems(findings...); err != nil {
		return PreparedMigration{}, err
	}
	return prepared, nil
}

func reportTargetPreflightProgress(progress func(TargetPreflightProgress), event TargetPreflightProgress) {
	if progress != nil {
		progress(event)
	}
}

func preparedMigrationUsesLegacyWodby1EnvVars(prepared PreparedMigration) bool {
	for _, app := range prepared.Apps {
		for _, variable := range app.StackConfiguration.EnvVars {
			if variable.Name == wodby1LegacyEnvVarsMarker && strings.EqualFold(strings.TrimSpace(variable.Value), "true") {
				return true
			}
		}
		for _, service := range app.StackConfiguration.Services {
			for _, variable := range service.EnvVars {
				if variable.Name == wodby1LegacyEnvVarsMarker && strings.EqualFold(strings.TrimSpace(variable.Value), "true") {
					return true
				}
			}
		}
	}
	return false
}

func targetServiceCapacityFindings(
	plan *Plan,
	prepared PreparedMigration,
	opts TargetPreflightOptions,
) []ReviewItem {
	if plan == nil || plan.Target.Subscription == nil || plan.Target.Subscription.Plan == nil {
		return []ReviewItem{{
			Severity: SeverityBlocking,
			Subject:  "target app-service capacity",
			Message:  "target Wodby 2 API did not return subscription usage and allowance; capacity cannot be verified safely",
		}}
	}
	// A resume can contain target services already included in live usage. The
	// backend repeats its atomic limit check for every remaining app/instance,
	// while recounting the entire saved plan here would double-count them.
	if opts.AllowedTargetAppID > 0 || opts.AllowStateBackedAppRecovery ||
		len(opts.AllowedTargetAppIDs) != 0 || len(opts.StateBackedAppRecovery) != 0 {
		return nil
	}
	subscription := plan.Target.Subscription
	if !strings.EqualFold(strings.TrimSpace(subscription.Status), "ACTIVE") &&
		!strings.EqualFold(strings.TrimSpace(subscription.Status), "CANCELING") {
		return []ReviewItem{{
			Severity: SeverityBlocking,
			Subject:  "target subscription",
			Message:  fmt.Sprintf("target subscription status %q cannot accept new app services", subscription.Status),
		}}
	}
	if !strings.EqualFold(strings.TrimSpace(subscription.Plan.Name), "developer") {
		return nil
	}
	additional := 0
	for _, app := range prepared.Apps {
		for _, instance := range app.Instances {
			for _, enabled := range instance.EffectiveState {
				if enabled {
					additional++
				}
			}
		}
	}
	projected := subscription.Plan.Usage + float64(additional)
	if projected <= subscription.Plan.UsageIncluded {
		return nil
	}
	return []ReviewItem{{
		Severity: SeverityBlocking,
		Subject:  "target app-service capacity",
		Message: fmt.Sprintf(
			"migration needs %d enabled target app service(s), which would raise free-plan usage from %.0f to %.0f; the current allowance is %.0f. Disable or remap optional services, remove other usage, or upgrade the target plan",
			additional,
			subscription.Plan.Usage,
			projected,
			subscription.Plan.UsageIncluded,
		),
	}}
}

// preflightWodbyCIPipelines checks every distinct source ref used by an app,
// while reporting a missing pipeline against each affected instance.
func (c *TargetClient) preflightWodbyCIPipelines(
	ctx context.Context,
	app PreparedAppMigration,
	repository *RepositoryPlan,
) ([]ReviewItem, error) {
	if repository == nil || repository.GitIntegrationID <= 0 ||
		strings.TrimSpace(repository.RemoteGitRepoID) == "" {
		return nil, nil
	}

	type refKey struct {
		ref     string
		refType string
	}
	presence := map[refKey]bool{}
	checked := map[refKey]bool{}
	findings := []ReviewItem{}
	for _, instance := range app.Instances {
		if strings.EqualFold(strings.TrimSpace(stringProperty(instance.Source.Properties, "deployment_type")), "ci") ||
			!instance.UsesWodbyCI || instance.BuildSource == nil || instance.BuildSource.Input.GitRef == nil ||
			instance.BuildSource.Input.GitRefType == nil {
			continue
		}
		key := refKey{
			ref:     strings.TrimSpace(*instance.BuildSource.Input.GitRef),
			refType: strings.TrimSpace(*instance.BuildSource.Input.GitRefType),
		}
		if !checked[key] {
			exists, err := c.RemoteGitRepoFileExists(
				ctx,
				repository.GitIntegrationID,
				repository.RemoteGitRepoID,
				wodbyCIPipelinePath,
				key.ref,
			)
			if err != nil {
				return nil, err
			}
			presence[key] = exists
			checked[key] = true
		}
		if presence[key] {
			findings = append(findings, ReviewItem{
				Severity: SeverityMigration,
				App:      app.App.App.Name,
				Instance: instance.Source.Name,
				Subject:  "Wodby CI pipeline",
				Message: fmt.Sprintf(
					"Wodby CI will be used by default; pipeline %q was found in repository %q at Git %s %q",
					wodbyCIPipelinePath,
					repository.RepositoryName,
					strings.ToLower(key.refType),
					key.ref,
				),
			})
			continue
		}

		message := fmt.Sprintf(
			"repository %q does not contain required Wodby CI pipeline %q at Git %s %q; add the pipeline before applying the migration",
			repository.RepositoryName,
			wodbyCIPipelinePath,
			strings.ToLower(key.refType),
			key.ref,
		)
		if guidance := wodbyCIPipelineGuidance(app.App.App, instance.Source); guidance != "" {
			message += ". " + guidance
		}
		findings = append(findings, ReviewItem{
			Severity: SeverityBlocking,
			App:      app.App.App.Name,
			Instance: instance.Source.Name,
			Subject:  "Wodby CI pipeline",
			Message:  message,
		})
	}
	return findings, nil
}

func wodbyCIPipelineGuidance(app App, instance Instance) string {
	candidates := []string{app.Type, instance.Stack.Type, instance.Stack.Name, instance.Stack.AncestorName}
	for _, candidate := range candidates {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		switch {
		case strings.Contains(normalized, "drupal"):
			return "For Drupal, copy and adapt https://github.com/wodby/drupal-vanilla/blob/11.x/.wodby/pipeline.yml and, if needed, https://github.com/wodby/drupal-vanilla/blob/11.x/.wodby/post-deployment.yml"
		case strings.Contains(normalized, "wordpress"):
			return "For WordPress, copy and adapt https://github.com/wodby/wordpress-vanilla/blob/main/.wodby/pipeline.yml and, if needed, https://github.com/wodby/wordpress-vanilla/blob/main/.wodby/post-deployment.yml; both are available under https://github.com/wodby/wordpress-vanilla/tree/main/.wodby"
		}
	}
	return ""
}

func (c *TargetClient) resolveRepositoryPlan(
	ctx context.Context,
	app App,
	plan *RepositoryPlan,
	skipCode bool,
) ([]ReviewItem, error) {
	if skipCode || plan == nil || plan.Action == "skip" ||
		plan.GitIntegrationID <= 0 || strings.TrimSpace(plan.RepositoryName) == "" {
		return nil, nil // The base plan records incomplete customer selections.
	}

	repositories, err := c.ListRemoteGitRepos(ctx, plan.GitIntegrationID)
	if err != nil {
		return nil, err
	}
	desiredName := strings.TrimSpace(plan.RepositoryName)
	matches := make([]TargetRemoteGitRepo, 0, 1)
	for _, repository := range repositories {
		if strings.TrimSpace(repository.Name) == desiredName {
			matches = append(matches, repository)
		}
	}
	if len(matches) == 0 {
		plan.Action = "unlinked"
		plan.RemoteGitRepoID = ""
		return []ReviewItem{{
			Severity: SeverityServiceWarning,
			App:      app.Name,
			Subject:  "repository",
			Message: fmt.Sprintf(
				"repository %q was not found in the selected Wodby 2 Git integration; the target will continue without a Git link and use Custom CI. Pass --target-repository-name with an exact name to link another repository (or --target-repository-map %s=GIT_INTEGRATION_ID:REPOSITORY_NAME for this app in a server migration)",
				desiredName,
				app.Name,
			),
		}}, nil
	}
	if len(matches) > 1 {
		plan.Action = "unlinked"
		plan.RemoteGitRepoID = ""
		return []ReviewItem{{
			Severity: SeverityServiceWarning,
			App:      app.Name,
			Subject:  "repository",
			Message: fmt.Sprintf(
				"repository name %q matched %d repositories in the selected Wodby 2 Git integration; the target will continue without a Git link and use Custom CI",
				desiredName,
				len(matches),
			),
		}}, nil
	}
	resolvedID := strings.TrimSpace(matches[0].ID)
	if pinnedID := strings.TrimSpace(plan.RemoteGitRepoID); pinnedID != "" && pinnedID != resolvedID {
		return nil, errors.Errorf(
			"reviewed target repository %q no longer matches remote repository ID %q in Wodby 2 Git integration ID %d",
			desiredName,
			pinnedID,
			plan.GitIntegrationID,
		)
	}
	plan.RemoteGitRepoID = resolvedID
	return nil, nil
}

func (c *TargetClient) preflightInstance(
	ctx context.Context,
	app App,
	source Instance,
	plan *InstancePlan,
	targetOrgID int,
	targetProjectID int,
	repositoryPlan *RepositoryPlan,
	opts TargetPreflightOptions,
	disableCronSchedules bool,
	disableCustomRoutes bool,
) (PreparedInstance, []ReviewItem, error) {
	pinned := plan.Stack.TargetRevID != 0
	stack, err := c.resolvePreflightStackRevision(
		ctx, targetOrgID, targetProjectID, plan.Stack,
	)
	if err != nil {
		return PreparedInstance{}, nil, errors.Wrapf(err, "resolve target stack for %s/%s", app.Name, source.Name)
	}
	stackManifest := stack.RevisionManifest
	if !pinned {
		stackRevision, err := c.GetStackRevision(ctx, stack.RevID)
		if err != nil {
			return PreparedInstance{}, nil, errors.Wrapf(err, "read target stack revision for %s/%s", app.Name, source.Name)
		}
		if stackRevision.StackID != stack.ID {
			return PreparedInstance{}, nil, errors.Errorf(
				"target stack revision ID %d belongs to stack ID %d, expected %d",
				stackRevision.ID,
				stackRevision.StackID,
				stack.ID,
			)
		}
		stackManifest = stackRevision.Manifest
	}
	stack.RevisionManifest = stackManifest
	inspections, err := c.InspectStackRevision(ctx, stack.RevID)
	if err != nil {
		return PreparedInstance{}, nil, errors.Wrapf(err, "inspect target stack for %s/%s", app.Name, source.Name)
	}
	byName, err := indexStackInspections(inspections)
	if err != nil {
		return PreparedInstance{}, nil, err
	}
	plan.Stack.Target = stack.Name
	plan.Stack.TargetID = stack.ID
	plan.Stack.TargetRevID = stack.RevID
	plan.Stack.TargetVersion = stackVersionLabel(stack)
	findings := []ReviewItem{}

	sourceByName := make(map[string]Service, len(source.Services))
	for _, service := range source.Services {
		sourceByName[service.Name] = service
	}
	effective := make(map[string]bool, len(inspections))
	for _, inspection := range inspections {
		effective[inspection.StackService.Name] = !inspection.StackService.Disabled
	}
	preparedServices := map[string]PreparedService{}
	targetOwners := map[string]string{}
	for index := range plan.Services {
		servicePlan := &plan.Services[index]
		sourceService, exists := sourceByName[servicePlan.SourceName]
		if !exists {
			return PreparedInstance{}, nil, errors.Errorf(
				"migration plan service %q is missing from source instance %q",
				servicePlan.SourceName,
				source.UUID,
			)
		}
		if servicePlan.TargetName == "" {
			continue
		}
		inspection, exists := byName[servicePlan.TargetName]
		if !exists {
			if !servicePlan.Enabled {
				continue
			}
			if !opts.AddMissingServices && !servicePlan.AddToStack {
				findings = append(findings, ReviewItem{
					Severity: SeverityBlocking,
					App:      app.Name,
					Instance: source.Name,
					Subject:  "service " + sourceService.Name,
					Message:  fmt.Sprintf("target stack has no service named %q; rerun with --add-missing-services or map it to an existing stack service", servicePlan.TargetName),
				})
				continue
			}
			catalogService, serviceRevision, resolveErr := c.ResolveServiceExact(ctx, targetOrgID, servicePlan.TargetName)
			if resolveErr != nil {
				var apiErr *rest.APIError
				if errors.As(resolveErr, &apiErr) && apiErr.StatusCode == 404 {
					findings = append(findings, ReviewItem{
						Severity: SeverityBlocking,
						App:      app.Name,
						Instance: source.Name,
						Subject:  "service " + sourceService.Name,
						Message:  fmt.Sprintf("target stack has no service named %q and no exact accessible Wodby 2 service could be added; use --target-service-map with an available service name", servicePlan.TargetName),
					})
					continue
				}
				return PreparedInstance{}, nil, resolveErr
			}
			if servicePlan.AddToStack {
				if servicePlan.CatalogServiceID != catalogService.ID || servicePlan.CatalogServiceRevID != serviceRevision.ID {
					return PreparedInstance{}, nil, errors.Errorf("reviewed additional service %q no longer matches service ID %d and revision ID %d", servicePlan.TargetName, servicePlan.CatalogServiceID, servicePlan.CatalogServiceRevID)
				}
			} else {
				servicePlan.AddToStack = true
				servicePlan.CatalogServiceID = catalogService.ID
				servicePlan.CatalogServiceRevID = serviceRevision.ID
			}
			inspection = TargetStackServiceInspection{
				StackService: TargetStackService{
					Name: servicePlan.TargetName, Title: catalogService.Title, Type: serviceRevision.Type,
					ServiceRevID: serviceRevision.ID, ServiceRevName: serviceRevision.Name,
					ServiceRevVersion: serviceRevision.Version, Replicas: 1,
				},
				ServiceRevision: serviceRevision,
			}
			byName[servicePlan.TargetName] = inspection
		}
		if sourceService.Enabled && sourceService.Name == "sshd" &&
			servicePlan.Action == "substitute" && !isPHPSSHDerivativeTarget(inspection) {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "service sshd",
				Message:  fmt.Sprintf("target service %q is not a PHP SSH derivative", servicePlan.TargetName),
			})
			continue
		}
		if pinned && !servicePlan.AddToStack {
			if servicePlan.TargetID <= 0 || servicePlan.TargetServiceRevID <= 0 {
				return PreparedInstance{}, nil, errors.Errorf(
					"reviewed target service %q is missing immutable IDs",
					servicePlan.TargetName,
				)
			}
			if servicePlan.TargetID != inspection.StackService.ID ||
				servicePlan.TargetServiceRevID != inspection.StackService.ServiceRevID {
				return PreparedInstance{}, nil, errors.Errorf(
					"reviewed target service %q no longer matches stack service ID %d and service revision ID %d",
					servicePlan.TargetName,
					servicePlan.TargetID,
					servicePlan.TargetServiceRevID,
				)
			}
		}
		if owner, duplicate := targetOwners[servicePlan.TargetName]; duplicate && owner != sourceService.Name {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "service mapping",
				Message:  fmt.Sprintf("source services %q and %q both map to target service %q", owner, sourceService.Name, servicePlan.TargetName),
			})
			continue
		}
		targetOwners[servicePlan.TargetName] = sourceService.Name
		if !servicePlan.AddToStack {
			servicePlan.TargetID = inspection.StackService.ID
			servicePlan.TargetServiceRevID = inspection.StackService.ServiceRevID
		}
		if servicePlan.Enabled {
			versionFinding, hasVersionFinding, err := resolveServiceVersion(
				servicePlan,
				inspection,
				stackManifest,
				time.Now().UTC(),
				pinned,
			)
			if err != nil {
				return PreparedInstance{}, nil, errors.Wrapf(err, "resolve target version for service %q", servicePlan.TargetName)
			}
			if hasVersionFinding {
				versionFinding.App = app.Name
				versionFinding.Instance = source.Name
				findings = append(findings, versionFinding)
			}
		} else {
			servicePlan.TargetVersion = ""
			servicePlan.VersionAction = ""
		}
		effective[inspection.StackService.Name] = servicePlan.Enabled
		if !servicePlan.Enabled && inspection.StackService.Required {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "service " + sourceService.Name,
				Message:  fmt.Sprintf("source service is disabled but target service %q is required", inspection.StackService.Name),
			})
			continue
		}
		replicas, resources, capacityFindings := prepareServiceCapacity(app.Name, source.Name, sourceService, inspection)
		findings = append(findings, capacityFindings...)
		preparedServices[sourceService.Name] = PreparedService{
			Source: sourceService, Target: inspection, TargetVersion: servicePlan.TargetVersion,
			Replicas: replicas, Resources: resources,
		}
	}

	prepared := PreparedInstance{
		Source:               source,
		SkipCode:             opts.SkipCode,
		Stack:                stack,
		StackServices:        inspections,
		Services:             preparedServices,
		Imports:              map[string]PreparedImport{},
		ImportByComponent:    map[string]PreparedImport{},
		EffectiveState:       effective,
		DisableCronSchedules: disableCronSchedules,
		DisableCustomRoutes:  disableCustomRoutes,
		TargetEnvType:        strings.ToUpper(strings.TrimSpace(plan.TargetEnvType)),
		UsesWodbyCI:          true,
	}
	for _, servicePlan := range plan.Services {
		if !servicePlan.AddToStack {
			continue
		}
		mapping, ok := preparedServices[servicePlan.SourceName]
		if !ok {
			continue
		}
		prepared.StackAdditions = append(prepared.StackAdditions, PreparedStackServiceAddition{
			Name: servicePlan.TargetName, Title: mapping.Target.StackService.Title,
			ServiceID: servicePlan.CatalogServiceID, ServiceRevisionID: servicePlan.CatalogServiceRevID,
			Inspection: mapping.Target,
		})
	}
	for _, sourceService := range source.Services {
		for _, variable := range sourceService.EnvVars {
			if variable.Enabled || !containsString(variable.OverrideFields, "enabled") {
				continue
			}
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "env var " + variable.Name,
				Message:  "a disabled inherited environment-variable override cannot be represented safely by the Wodby 2 environment API",
			})
		}
	}
	buildSource, buildFindings := prepareBuildSource(app, source, repositoryPlan, inspections, effective, opts)
	prepared.BuildSource = buildSource
	findings = append(findings, buildFindings...)
	if repositoryPlan != nil && buildSource != nil {
		inspection := byName[buildSource.ServiceName]
		if pinned {
			if plan.BuildServiceID != inspection.StackService.ID ||
				plan.BuildServiceRevID != inspection.StackService.ServiceRevID {
				return PreparedInstance{}, nil, errors.Errorf(
					"reviewed build service %q no longer matches stack service ID %d and service revision ID %d",
					buildSource.ServiceName,
					plan.BuildServiceID,
					plan.BuildServiceRevID,
				)
			}
		}
		if repositoryPlan.TargetService == "" {
			repositoryPlan.TargetService = buildSource.ServiceName
		}
		plan.BuildServiceID = inspection.StackService.ID
		plan.BuildServiceRevID = inspection.StackService.ServiceRevID
	}

	if !opts.SkipData {
		for index := range plan.Imports {
			importPlan := &plan.Imports[index]
			sourceBackup, found := findBackupByUUID(source.Backups, importPlan.SourceUUID)
			if !found {
				return PreparedInstance{}, nil, errors.Errorf(
					"migration plan backup %q is missing from source instance %q",
					importPlan.SourceUUID,
					source.UUID,
				)
			}
			destination, review := resolveImportDestination(
				app,
				source,
				sourceBackup,
				importPlan,
				inspections,
				effective,
			)
			findings = append(findings, review...)
			if destination != nil {
				if pinned &&
					(importPlan.TargetServiceID != destination.StackService.StackService.ID ||
						importPlan.TargetServiceRevID != destination.StackService.StackService.ServiceRevID) {
					return PreparedInstance{}, nil, errors.Errorf(
						"reviewed import %q no longer matches stack service ID %d and service revision ID %d",
						importPlan.Component,
						importPlan.TargetServiceID,
						importPlan.TargetServiceRevID,
					)
				}
				importPlan.TargetServiceRevID = destination.StackService.StackService.ServiceRevID
				prepared.Imports[sourceBackup.UUID] = *destination
				component := strings.ToLower(strings.TrimSpace(sourceBackup.Component))
				if _, exists := prepared.ImportByComponent[component]; exists {
					findings = append(findings, ReviewItem{
						Severity: SeverityBlocking,
						App:      app.Name,
						Instance: source.Name,
						Subject:  "backup " + sourceBackup.Component,
						Message:  "source export contains multiple files for the same backup component",
					})
				} else {
					prepared.ImportByComponent[component] = *destination
				}
			}
		}
	}
	certificateFindings, err := c.resolveCustomRouteCertificates(ctx, app, source, plan, targetOrgID, pinned)
	if err != nil {
		return PreparedInstance{}, nil, err
	}
	findings = append(findings, certificateFindings...)
	return prepared, findings, nil
}

func (c *TargetClient) resolveCustomRouteCertificates(
	ctx context.Context,
	app App,
	instance Instance,
	plan *InstancePlan,
	targetOrgID int,
	pinned bool,
) ([]ReviewItem, error) {
	if plan == nil {
		return nil, errors.New("instance migration plan is required")
	}
	findings := []ReviewItem{}
	for index := range plan.Routes {
		route := &plan.Routes[index]
		if !route.SSLCustom || (route.Action != "create_backend" && route.Action != "create_redirect") {
			continue
		}
		certificates, err := c.ListMatchingCustomCerts(ctx, targetOrgID, route.Host)
		if err != nil {
			return nil, errors.Wrapf(err, "resolve custom certificate for route %q", route.Host)
		}
		if pinned {
			if route.TargetCertID > 0 {
				matched := false
				for _, certificate := range certificates {
					if certificate.ID == route.TargetCertID {
						matched = true
						break
					}
				}
				if !matched {
					return nil, errors.Errorf(
						"reviewed Wodby 2 custom certificate ID %d no longer actively covers route %q; restore or replace the certificate, then start a new migration plan before target changes",
						route.TargetCertID,
						route.Host,
					)
				}
				findings = append(findings, customCertificateMigrationReview(app, instance, *route))
				continue
			}
			findings = append(findings, customCertificateManualReview(app, instance, route.Host, len(certificates)))
			continue
		}

		switch len(certificates) {
		case 1:
			route.TargetCertID = certificates[0].ID
			route.TargetCertDNSNames = normalizedCertificateDNSNames(certificates[0])
			findings = append(findings, customCertificateMigrationReview(app, instance, *route))
		default:
			route.TargetCertID = 0
			route.TargetCertDNSNames = nil
			findings = append(findings, customCertificateManualReview(app, instance, route.Host, len(certificates)))
		}
	}
	return findings, nil
}

func customCertificateMigrationReview(app App, instance Instance, route RoutePlan) ReviewItem {
	hostnames := strings.Join(route.TargetCertDNSNames, ", ")
	if hostnames == "" {
		hostnames = "hostname metadata unavailable"
	}
	return ReviewItem{
		Severity: SeverityMigration,
		App:      app.Name,
		Instance: instance.Name,
		Subject:  "route " + route.Host + " custom TLS",
		Message: fmt.Sprintf(
			"existing Wodby 2 custom certificate ID %d matches this hostname and will be attached (certificate hostnames: %s)",
			route.TargetCertID,
			hostnames,
		),
	}
}

func normalizedCertificateDNSNames(certificate TargetCert) []string {
	names := append([]string(nil), certificate.DNSNames...)
	if domain := strings.TrimSpace(certificate.Domain); domain != "" {
		names = append(names, domain)
	}
	seen := map[string]bool{}
	normalized := names[:0]
	for _, name := range names {
		name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized
}

func customCertificateManualReview(app App, instance Instance, host string, matches int) ReviewItem {
	reason := "no active Wodby 2 custom certificate covers this hostname"
	if matches == 1 {
		reason = "an active matching Wodby 2 custom certificate exists now, but it was not selected by the saved migration plan"
	} else if matches > 1 {
		reason = fmt.Sprintf("%d active Wodby 2 custom certificates cover this hostname, so none can be selected safely", matches)
	}
	return ReviewItem{
		Severity: SeverityServiceWarning,
		App:      app.Name,
		Instance: instance.Name,
		Subject:  "route " + host + " custom TLS",
		Message:  reason + "; the route will be created without TLS. Add a custom certificate in Wodby 2 and attach it to this route before DNS cutover",
	}
}

func (c *TargetClient) resolvePreflightStackRevision(
	ctx context.Context,
	targetOrgID int,
	_ int,
	plan StackPlan,
) (TargetStack, error) {
	if plan.CreateTarget {
		if strings.TrimSpace(plan.CatalogName) == "" {
			return TargetStack{}, errors.New("generated target stack is missing its catalog name")
		}
		if plan.TargetID <= 0 {
			return c.ResolvePublicStackExact(ctx, plan.CatalogName)
		}
		stack, err := c.GetStack(ctx, plan.TargetID)
		if err != nil {
			return TargetStack{}, err
		}
		if !stack.Public || stack.Name != plan.CatalogName {
			return TargetStack{}, errors.Errorf(
				"reviewed catalog stack ID %d is %q (public=%t), expected public stack %q",
				stack.ID,
				stack.Name,
				stack.Public,
				plan.CatalogName,
			)
		}
		if plan.TargetRevID > 0 && stack.RevID != plan.TargetRevID {
			revision, err := c.GetStackRevision(ctx, plan.TargetRevID)
			if err != nil {
				return TargetStack{}, err
			}
			if revision.StackID != stack.ID {
				return TargetStack{}, errors.Errorf("reviewed catalog revision ID %d does not belong to catalog stack ID %d", revision.ID, stack.ID)
			}
			stack.RevID = revision.ID
			stack.LatestRevNumber = revision.Number
		}
		return stack, nil
	}
	if plan.TargetID <= 0 {
		return TargetStack{}, errors.New("explicit target stack ID is required")
	}
	if plan.TargetRevID == 0 {
		stack, err := c.GetStack(ctx, plan.TargetID)
		if err != nil {
			return TargetStack{}, err
		}
		if stack.OrgID != targetOrgID {
			return TargetStack{}, errors.Errorf(
				"selected target stack ID %d belongs to organization ID %d, expected %d",
				stack.ID,
				stack.OrgID,
				targetOrgID,
			)
		}
		return stack, nil
	}
	if strings.TrimSpace(plan.Target) == "" {
		return TargetStack{}, errors.New("reviewed target stack pins are incomplete")
	}
	stack, err := c.GetStack(ctx, plan.TargetID)
	if err != nil {
		return TargetStack{}, err
	}
	if stack.OrgID != targetOrgID {
		return TargetStack{}, errors.Errorf(
			"reviewed target stack ID %d belongs to organization ID %d, expected %d",
			stack.ID,
			stack.OrgID,
			targetOrgID,
		)
	}
	if stack.Name != plan.Target {
		return TargetStack{}, errors.Errorf(
			"reviewed target stack ID %d is named %q, expected %q",
			stack.ID,
			stack.Name,
			plan.Target,
		)
	}
	revision, err := c.GetStackRevision(ctx, plan.TargetRevID)
	if err != nil {
		return TargetStack{}, err
	}
	if revision.StackID != stack.ID {
		return TargetStack{}, errors.Errorf(
			"reviewed stack revision ID %d belongs to stack ID %d, expected %d",
			revision.ID,
			revision.StackID,
			stack.ID,
		)
	}
	version := fmt.Sprintf("revision-%d", revision.Number)
	if plan.TargetVersion != version {
		return TargetStack{}, errors.Errorf(
			"reviewed stack revision ID %d has version label %q, expected %q",
			revision.ID,
			version,
			plan.TargetVersion,
		)
	}
	stack.RevID = revision.ID
	stack.LatestRevNumber = revision.Number
	stack.RevisionManifest = revision.Manifest
	return stack, nil
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func indexStackInspections(items []TargetStackServiceInspection) (map[string]TargetStackServiceInspection, error) {
	result := make(map[string]TargetStackServiceInspection, len(items))
	for _, item := range items {
		name := item.StackService.Name
		if _, exists := result[name]; exists {
			return nil, &TargetAmbiguousMatchError{Resource: "stack service", Name: name, Count: 2}
		}
		result[name] = item
	}
	return result, nil
}

func isTargetServiceDerivative(item TargetStackServiceInspection) bool {
	stackType := strings.ToLower(strings.TrimSpace(item.StackService.Type))
	revisionType := strings.ToLower(strings.TrimSpace(item.ServiceRevision.Type))
	return stackType != "" && revisionType != "" && stackType != revisionType
}

func isPHPSSHDerivativeTarget(item TargetStackServiceInspection) bool {
	if !isTargetServiceDerivative(item) || !strings.EqualFold(strings.TrimSpace(item.StackService.Type), "ssh") {
		return false
	}
	serviceName := item.ServiceRevision.Name
	if item.ServiceRevision.Manifest != nil && strings.TrimSpace(item.ServiceRevision.Manifest.Name) != "" {
		serviceName = item.ServiceRevision.Manifest.Name
	}
	serviceName = strings.ToLower(strings.TrimSpace(serviceName))
	return serviceName == "php" || strings.Contains(serviceName, "-php")
}

func prepareBuildSource(
	app App,
	instance Instance,
	repositoryPlan *RepositoryPlan,
	inspections []TargetStackServiceInspection,
	effective map[string]bool,
	opts TargetPreflightOptions,
) (*PreparedBuildSource, []ReviewItem) {
	if opts.SkipCode {
		return nil, nil
	}
	buildServices := make([]TargetStackServiceInspection, 0, 1)
	for _, inspection := range inspections {
		if isTargetServiceDerivative(inspection) ||
			inspection.ServiceRevision.Manifest == nil ||
			inspection.ServiceRevision.Manifest.Build == nil ||
			!inspection.ServiceRevision.Manifest.Build.Connect ||
			!effective[inspection.StackService.Name] {
			continue
		}
		buildServices = append(buildServices, inspection)
	}
	var selected TargetStackServiceInspection
	serviceSelector := strings.TrimSpace(opts.CodeService)
	if repositoryPlan != nil && strings.TrimSpace(repositoryPlan.TargetService) != "" {
		serviceSelector = strings.TrimSpace(repositoryPlan.TargetService)
	}
	if serviceSelector != "" {
		for _, candidate := range buildServices {
			if candidate.StackService.Name == serviceSelector {
				selected = candidate
				break
			}
		}
		if selected.StackService.ID == 0 {
			return nil, []ReviewItem{{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: instance.Name,
				Subject:  "repository target service",
				Message:  fmt.Sprintf("target service %q is not an enabled connect-build service", serviceSelector),
			}}
		}
	} else if len(buildServices) == 1 {
		selected = buildServices[0]
	} else if len(buildServices) == 0 {
		return nil, nil
	} else {
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "repository target service",
			Message:  fmt.Sprintf("target stack has %d enabled connect-build services; select one with --target-code-service", len(buildServices)),
		}}
	}

	connected := app.Repository != nil && repositoryPlan != nil && repositoryPlan.Action == "connect" &&
		repositoryPlan.GitIntegrationID > 0 && strings.TrimSpace(repositoryPlan.RepositoryName) != "" &&
		strings.TrimSpace(repositoryPlan.RemoteGitRepoID) != ""
	if !connected {
		return &PreparedBuildSource{ServiceName: selected.StackService.Name}, []ReviewItem{{
			Severity: SeverityMigration,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "application code",
			Message:  fmt.Sprintf("target service %q will use third-party Custom CI without a linked Git repository", selected.StackService.Name),
		}}
	}

	gitRef := strings.TrimSpace(opts.GitRef)
	if gitRef == "" {
		gitRef = stringProperty(instance.Properties, "git_target_value")
	}
	gitRefType := strings.TrimSpace(opts.GitRefType)
	if gitRefType == "" {
		gitRefType = stringProperty(instance.Properties, "git_target_type")
	}
	gitRefType = normalizeGitRefType(gitRefType)
	if gitRef == "" || gitRefType == "" {
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "repository ref",
			Message:  "source Git ref and ref type are required; pass --target-git-ref and --target-git-ref-type",
		}}
	}
	if deploymentType := strings.ToLower(stringProperty(instance.Properties, "deployment_type")); deploymentType != "" && deploymentType != "git" && deploymentType != "ci" {
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "deployment type",
			Message:  fmt.Sprintf("source deployment type %q is not supported by the Git migration path", deploymentType),
		}}
	}

	integrationID := repositoryPlan.GitIntegrationID
	remoteRepoID := repositoryPlan.RemoteGitRepoID
	return &PreparedBuildSource{
			ServiceName: selected.StackService.Name,
			Input: TargetBuildSourceInput{
				BuildSourceType: TargetBuildSourceConnect,
				IntegrationID:   &integrationID,
				RemoteGitRepoID: &remoteRepoID,
				GitRef:          &gitRef,
				GitRefType:      &gitRefType,
			},
		}, []ReviewItem{{
			Severity: SeverityMigration,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "repository build source",
			Message:  fmt.Sprintf("target service %q will build Git %s %q", selected.StackService.Name, strings.ToLower(gitRefType), gitRef),
		}}
}

func resolveImportDestination(
	app App,
	instance Instance,
	backup Backup,
	plan *ImportPlan,
	inspections []TargetStackServiceInspection,
	effective map[string]bool,
) (*PreparedImport, []ReviewItem) {
	expectedImport := sourceComponentImportName(backup.Component)
	if expectedImport == "" {
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "backup " + backup.Component,
			Message:  fmt.Sprintf("source backup component %q has no supported Wodby 2 import mapping", backup.Component),
		}}
	}

	candidates := make([]PreparedImport, 0, 2)
	for _, inspection := range inspections {
		if !effective[inspection.StackService.Name] || inspection.ServiceRevision.Manifest == nil {
			continue
		}
		for _, capability := range inspection.ServiceRevision.Manifest.Imports {
			if capability.Name != expectedImport {
				continue
			}
			candidates = append(candidates, PreparedImport{
				Source:       backup,
				ServiceName:  inspection.StackService.Name,
				ImportName:   capability.Name,
				StackService: inspection,
			})
		}
	}
	targetService := plan.TargetService
	targetImport := plan.TargetImport
	defaultFilesMapping := targetService == "" && targetImport == "" && expectedImport == "files"
	if defaultFilesMapping {
		targetService = "files-nfs"
		targetImport = "files"
	}
	if targetService != "" || targetImport != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.ServiceName == targetService && candidate.ImportName == targetImport {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	if len(candidates) != 1 {
		message := fmt.Sprintf(
			"backup component %q matched %d enabled target imports; provide an unambiguous --target-import-map",
			backup.Component,
			len(candidates),
		)
		if defaultFilesMapping {
			message = fmt.Sprintf(
				"backup component %q requires enabled target service %q capability %q; matched %d",
				backup.Component,
				targetService,
				targetImport,
				len(candidates),
			)
		}
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "backup " + backup.Component,
			Message:  message,
		}}
	}
	selected := candidates[0]
	plan.TargetService = selected.ServiceName
	plan.TargetImport = selected.ImportName
	plan.TargetServiceID = selected.StackService.StackService.ID
	plan.TargetServiceRevID = selected.StackService.StackService.ServiceRevID
	return &selected, nil
}

func findBackupByUUID(items []Backup, uuid string) (Backup, bool) {
	for _, item := range items {
		if item.UUID == uuid {
			return item, true
		}
	}
	return Backup{}, false
}

func sourceComponentImportName(component string) string {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "db", "database":
		return "database"
	case "files":
		return "files"
	default:
		return ""
	}
}

func stringProperty(properties map[string]interface{}, name string) string {
	value, found := properties[name]
	if !found || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func normalizeGitRefType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "branch":
		return TargetGitRefBranch
	case "tag":
		return TargetGitRefTag
	case "commit", "sha":
		return TargetGitRefCommit
	default:
		return ""
	}
}

func stackVersionLabel(stack TargetStack) string {
	if stack.LatestRevNumber > 0 {
		return fmt.Sprintf("revision-%d", stack.LatestRevNumber)
	}
	return ""
}
