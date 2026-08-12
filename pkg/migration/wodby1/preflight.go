package wodby1

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
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
	AllowedTargetAppID          int
	AllowStateBackedAppRecovery bool
	AllowedTargetAppIDs         map[string]int
	StateBackedAppRecovery      map[string]bool
}

// PreparedMigration is an in-memory, secret-free target mapping. It is rebuilt
// from the approved plan on every phase instead of being persisted in the
// state file. Mutation phases pin and read back the reviewed immutable IDs.
type PreparedMigration struct {
	App       AppExport
	Instances []PreparedInstance
	Apps      []PreparedAppMigration
}

type PreparedAppMigration struct {
	App       AppExport
	Instances []PreparedInstance
}

// ForApp returns the single-app target mapping consumed by MigrationExecutor.
func (p PreparedMigration) ForApp(sourceUUID string) (PreparedMigration, bool) {
	for _, app := range p.Apps {
		if app.App.App.UUID == sourceUUID {
			return PreparedMigration{App: app.App, Instances: app.Instances, Apps: []PreparedAppMigration{app}}, true
		}
	}
	if p.App.App.UUID == sourceUUID {
		return PreparedMigration{App: p.App, Instances: p.Instances}, true
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
}

type PreparedService struct {
	Source        Service
	Target        TargetStackServiceInspection
	TargetVersion string
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
	for _, appExport := range appExports {
		appPlan := planApps[appExport.App.UUID]
		if appPlan == nil {
			return PreparedMigration{}, errors.Errorf("migration plan is missing source app %q", appExport.App.UUID)
		}
		existingApp, appFound, err := c.FindAppExact(ctx, plan.Target.OrgID, appExport.App.Name)
		if err != nil {
			return PreparedMigration{}, errors.Wrap(err, "check target app name availability")
		}
		allowedTargetAppID := opts.AllowedTargetAppID
		allowRecovery := opts.AllowStateBackedAppRecovery
		if plan.Source.Kind == "server" {
			allowedTargetAppID = opts.AllowedTargetAppIDs[appExport.App.UUID]
			allowRecovery = opts.StateBackedAppRecovery[appExport.App.UUID]
		}
		if appFound && existingApp.ID != allowedTargetAppID && !allowRecovery {
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
		for _, sourceInstance := range appExport.Instances {
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
		sort.SliceStable(preparedApp.Instances, func(i, j int) bool {
			return compareInstance(preparedApp.Instances[i].Source, preparedApp.Instances[j].Source) < 0
		})
		if plan.Target.CIIntegrationID == 0 && !opts.SkipCode {
			pipelineFindings, err := c.preflightWodbyCIPipelines(ctx, preparedApp, appPlan.Repository)
			if err != nil {
				return PreparedMigration{}, err
			}
			findings = append(findings, pipelineFindings...)
		}
		prepared.Apps = append(prepared.Apps, preparedApp)
	}
	if len(prepared.Apps) == 1 {
		prepared.App = prepared.Apps[0].App
		prepared.Instances = prepared.Apps[0].Instances
	}
	findings = append(findings, targetServiceCapacityFindings(plan, prepared, opts)...)
	if err := plan.AddReviewItems(findings...); err != nil {
		return PreparedMigration{}, err
	}
	return prepared, nil
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
		if instance.BuildSource == nil || instance.BuildSource.Input.GitRef == nil ||
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
	candidates := []string{app.Type, instance.Stack.Name, instance.Stack.AncestorName}
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
		return []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Subject:  "repository",
			Message: fmt.Sprintf(
				"repository %q was not found in the selected Wodby 2 Git integration; pass --target-repository-name with an exact name exposed by that integration (or --target-repository-map %s=GIT_INTEGRATION_ID:REPOSITORY_NAME for this app in a server migration)",
				desiredName,
				app.Name,
			),
		}}, nil
	}
	if len(matches) > 1 {
		return []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Subject:  "repository",
			Message: fmt.Sprintf(
				"repository name %q matched %d repositories in the selected Wodby 2 Git integration; the integration must expose a unique exact name",
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
	return []ReviewItem{{
		Severity: SeverityConfirmation,
		App:      app.Name,
		Subject:  "repository",
		Message: fmt.Sprintf(
			"repository %q will be connected through the selected Wodby 2 Git integration when the target app is configured",
			desiredName,
		),
	}}, nil
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
	findings := []ReviewItem{{
		Severity: SeverityConfirmation,
		App:      app.Name,
		Instance: source.Name,
		Subject:  "target stack revision",
		Message: fmt.Sprintf(
			"target stack %s (%s) will be used",
			stack.Name,
			plan.Stack.TargetVersion,
		),
	}}

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
			if sourceService.Enabled {
				findings = append(findings, ReviewItem{
					Severity: SeverityBlocking,
					App:      app.Name,
					Instance: source.Name,
					Subject:  "service " + sourceService.Name,
					Message:  fmt.Sprintf("target stack has no service named %q", servicePlan.TargetName),
				})
			}
			continue
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
		if pinned {
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
		servicePlan.TargetID = inspection.StackService.ID
		servicePlan.TargetServiceRevID = inspection.StackService.ServiceRevID
		if sourceService.Enabled {
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
		effective[inspection.StackService.Name] = sourceService.Enabled
		if !sourceService.Enabled && inspection.StackService.Required {
			findings = append(findings, ReviewItem{
				Severity: SeverityBlocking,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "service " + sourceService.Name,
				Message:  fmt.Sprintf("source service is disabled but target service %q is required", inspection.StackService.Name),
			})
			continue
		}
		preparedServices[sourceService.Name] = PreparedService{
			Source: sourceService, Target: inspection, TargetVersion: servicePlan.TargetVersion,
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
		if len(source.Backups) == 0 {
			findings = append(findings, ReviewItem{
				Severity: SeverityConfirmation,
				App:      app.Name,
				Instance: source.Name,
				Subject:  "fresh source backup",
				Message:  "create a fresh Wodby 1 backup after enabling maintenance mode before sync-data",
			})
		}
	}
	return prepared, findings, nil
}

func (c *TargetClient) resolvePreflightStackRevision(
	ctx context.Context,
	targetOrgID int,
	_ int,
	plan StackPlan,
) (TargetStack, error) {
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
	if app.Repository == nil {
		if len(buildServices) == 0 {
			return nil, nil
		}
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "application code",
			Message:  "the target stack requires a build source but the Wodby 1 app has no repository; use --skip-code only for an intentional partial migration",
		}}
	}
	if repositoryPlan == nil || repositoryPlan.GitIntegrationID <= 0 ||
		strings.TrimSpace(repositoryPlan.RepositoryName) == "" ||
		strings.TrimSpace(repositoryPlan.RemoteGitRepoID) == "" {
		return nil, nil // The base plan already records the blocking mapping.
	}

	var selected TargetStackServiceInspection
	serviceSelector := strings.TrimSpace(repositoryPlan.TargetService)
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
	} else {
		return nil, []ReviewItem{{
			Severity: SeverityBlocking,
			App:      app.Name,
			Instance: instance.Name,
			Subject:  "repository target service",
			Message:  fmt.Sprintf("target stack has %d enabled connect-build services; select one with --target-code-service", len(buildServices)),
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
	if deploymentType := strings.ToLower(stringProperty(instance.Properties, "deployment_type")); deploymentType != "" && deploymentType != "git" {
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
			Severity: SeverityConfirmation,
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
	return &selected, []ReviewItem{{
		Severity: SeverityConfirmation,
		App:      app.Name,
		Instance: instance.Name,
		Subject:  "backup " + backup.Component,
		Message:  fmt.Sprintf("backup will import through target service %q capability %q", selected.ServiceName, selected.ImportName),
	}}
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
