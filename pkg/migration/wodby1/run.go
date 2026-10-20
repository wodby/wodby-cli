package wodby1

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/pkg/errors"
)

type migrationOutput struct {
	Action    string `json:"action"`
	Plan      Plan   `json:"plan"`
	PlanFile  string `json:"planFile"`
	StateFile string `json:"stateFile"`
	Status    string `json:"status"`
}

type serverAppMigrationOutput struct {
	SourceAppUUID string `json:"sourceAppUuid"`
	Name          string `json:"name"`
	StateFile     string `json:"stateFile"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}

type serverMigrationOutput struct {
	Action   string                     `json:"action"`
	Plan     Plan                       `json:"plan"`
	PlanFile string                     `json:"planFile"`
	Apps     []serverAppMigrationOutput `json:"apps"`
}

type serverAppExecution struct {
	sourceAppUUID string
	name          string
	statePath     string
	export        Export
	plan          Plan
	prepared      PreparedMigration
	executor      *MigrationExecutor
}

// resolveWorkspaceInstances returns the UUIDs of the selected source instances
// to migrate as development workspaces. An instance migration selects its only
// instance with --workspace; app and server migrations name instances.
func resolveWorkspaceInstances(export Export, sourceKind string, opts *RunOptions) (map[string]bool, error) {
	class, service := strings.TrimSpace(opts.WorkspaceStorageClass), strings.TrimSpace(opts.WorkspaceStorageService)
	if class != "" && service != "" {
		return nil, errors.New("--workspace-storage-class and --workspace-storage-service cannot be used together")
	}
	if (class != "" || service != "") && !opts.Workspace && len(opts.WorkspaceInstances) == 0 {
		return nil, errors.New("workspace storage options need an instance selected as a development workspace")
	}
	if opts.Workspace {
		result := map[string]bool{}
		for _, app := range export.AppExports() {
			for _, instance := range app.Instances {
				result[instance.UUID] = true
			}
		}
		return result, nil
	}
	return ResolveWorkspaceInstances(export, sourceKind, opts.WorkspaceInstances)
}

func runWodby1Single(cmd *RunEnv, sourceKind string, sourceID string, opts *RunOptions) (runErr error) {
	opts.SourceToken = firstNonBlank(opts.SourceToken, "")
	if err := validateOptions(opts); err != nil {
		return err
	}
	var err error
	opts.SourceToken, err = NormalizeSourceToken(opts.SourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.TargetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseTargetStackMapping(opts.TargetStackMap)
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.TargetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	versionMap, err := parseMapping(opts.TargetVersionMap, "--target-version-map")
	if err != nil {
		return err
	}
	importMap, err := parseMapping(opts.TargetImportMap, "--target-import-map")
	if err != nil {
		return err
	}
	sourceClient, err := NewSourceClient(opts.SourceBaseURL, opts.SourceToken)
	if err != nil {
		return err
	}

	artifacts, err := cmd.artifacts(sourceKind, sourceID, opts.StateFile)
	if err != nil {
		return err
	}
	planPath, statePath := artifacts.PlanLocation(), artifacts.StateLocation()
	// From this point onward, failures describe migration state, source or target
	// API behavior, or review blockers. Reprinting command usage for those errors
	// makes them look like invalid arguments and obscures the actionable output.
	cmd.silenceUsage()

	var reviewedPlan *Plan
	var restartStateIdentity *MigrationStateIdentity
	var restartStateWithMutations *MigrationState
	restartStateAppName := sourceID
	var resumeState *MigrationState
	var incompatiblePlan *UnsupportedMigrationPlanSchemaError
	allowedTargetAppID := 0
	allowTargetAppRecovery := false
	stateExists, err := artifacts.Exists(statePath)
	if err != nil {
		return err
	}
	planExists, err := artifacts.Exists(planPath)
	if err != nil {
		return err
	}
	if opts.Rollback {
		if !stateExists {
			return errors.Errorf(
				"no migration state found at %s; there is nothing this migration created to roll back",
				statePath,
			)
		}
		return runMigrationRollback(cmd, opts, artifacts, planPath, planExists, statePath)
	}
	if opts.Verify && !stateExists {
		return errors.Errorf("no applied migration state found at %s; run the same command with --apply first", statePath)
	}
	if opts.Restart && !stateExists {
		return errors.Errorf("--restart found no applied migration state at %s; run --apply without --restart to start a new migration", statePath)
	}
	if (opts.Apply || opts.Verify) && stateExists {
		if !planExists {
			return errors.Errorf(
				"applied migration state exists at %s but its plan is missing at %s; restore the plan before resuming",
				statePath,
				planPath,
			)
		}
		reviewed, err := artifacts.LoadPlan()
		if err != nil {
			if !errors.As(err, &incompatiblePlan) {
				return errors.Wrap(err, "load applied migration plan")
			}
			if incompatiblePlan.SourceKind != sourceKind || incompatiblePlan.SourceID != sourceID {
				return errors.Errorf("incompatible applied migration plan does not match the requested source %s", sourceKind)
			}
			state, stateErr := artifacts.InspectState(statePath)
			if stateErr != nil {
				return errors.Wrap(stateErr, "inspect migration state for incompatible plan")
			}
			if state.Source.Kind != sourceKind || state.Source.ID != sourceID {
				return errors.New("migration state does not match the source recorded by the incompatible plan")
			}
			if !opts.Restart {
				return incompatiblePlanRestartError(incompatiblePlan, planPath, []string{statePath}, []*MigrationState{state})
			}
			if state.CanRestartSafely() {
				identity := state.Identity()
				restartStateIdentity = &identity
			} else {
				if _, err := PlanRollback(state); err != nil {
					return errors.Wrap(err, "prepare --restart cleanup")
				}
				restartStateWithMutations = state
			}
		} else {
			if reviewed.Source.Kind != sourceKind || reviewed.Source.ID != sourceID {
				return errors.Errorf("applied migration plan does not match the requested source %s", sourceKind)
			}
			state, err := artifacts.LoadState(statePath, migrationStateIdentity(reviewed))
			if err != nil {
				return errors.Wrap(err, "load migration state before target preflight")
			}
			if opts.Restart {
				restartStateAppName = reviewedPlanAppName(reviewed)
				if state.CanRestartSafely() {
					identity := state.Identity()
					restartStateIdentity = &identity
				} else {
					if _, err := PlanRollback(state); err != nil {
						return errors.Wrap(err, "prepare --restart cleanup")
					}
					restartStateWithMutations = state
				}
			} else {
				if reviewed.Status == "blocked" || reviewed.Summary.Blocking != 0 {
					return errors.Errorf(
						"applied migration plan is blocked by %d review item(s); inspect %s",
						reviewed.Summary.Blocking,
						planPath,
					)
				}
				if opts.Apply && state.CanRestartSafely() {
					identity := state.Identity()
					restartStateIdentity = &identity
				}
				allowedTargetAppID, allowTargetAppRecovery = stateBackedTargetAppState(state)
				reviewedPlan = &reviewed
				resumeState = state
			}
		}
	}
	if opts.Apply && resumeState != nil {
		printSingleResumeNotice(cmd, planPath, statePath, resumeState)
		if resumeState.App.TargetID > 0 {
			printSingleResumeTargetValidation(cmd, resumeState.App.TargetID)
		}
	}

	targetClient, err := cmd.targetClient()
	if err != nil {
		return err
	}
	preparation := newMigrationPreparationProgress(cmd, sourceKind)
	preparation.StartStep("Discover Wodby 2 target scope")

	// Verify the caller's role in the selected destination organization before
	// asking Wodby 1 for the secret-bearing export.
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), TargetScopeSelectors{
		Project: opts.TargetProject,
		Cluster: opts.TargetCluster,
	})
	if err != nil {
		return err
	}
	if restartStateWithMutations != nil {
		if scope.Org.ID != restartStateWithMutations.Target.OrgID ||
			scope.Cluster.ID != restartStateWithMutations.Target.ClusterID {
			return errors.Errorf(
				"cannot restart the saved migration because the current target resolves to organization ID %d and cluster ID %d instead of the saved organization ID %d and cluster ID %d; use the original target credentials and options",
				scope.Org.ID,
				scope.Cluster.ID,
				restartStateWithMutations.Target.OrgID,
				restartStateWithMutations.Target.ClusterID,
			)
		}
		allowedTargetAppID, allowTargetAppRecovery = stateBackedTargetAppState(restartStateWithMutations)
	}
	if resumeState != nil && resumeState.App.TargetID > 0 {
		if scope.Org.ID != resumeState.Target.OrgID {
			return errors.Errorf(
				"saved migration target belongs to organization ID %d, but the current target resolves to organization ID %d; rerun with the original target credentials and options",
				resumeState.Target.OrgID,
				scope.Org.ID,
			)
		}
		_, found, err := targetClient.FindAppByID(cmd.Context(), resumeState.App.TargetID)
		if err != nil {
			return errors.Wrap(err, "verify saved target app before resume")
		}
		if !found {
			if resumeState.Target.ExistingApp {
				return errors.Errorf(
					"selected target app ID %d no longer exists, so this migration cannot continue; start a new preview with a different --target-app selector",
					resumeState.Target.AppID,
				)
			}
			return errors.Errorf(
				"saved target app ID %d no longer exists, so the saved migration plan cannot continue\nNext step: rerun the same command with --apply --restart to start from scratch",
				resumeState.App.TargetID,
			)
		}
		if opts.Apply {
			printSingleResumeTargetValidated(cmd)
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Target %s / %s is ready.", scope.Org.Name, scope.Cluster.Name))

	var targetApp *TargetApp
	targetStackID := opts.TargetStackID
	if selector := strings.TrimSpace(opts.TargetApp); selector != "" {
		preparation.StartStep("Resolve existing Wodby 2 target app and stack")
		if sourceKind != "instance" {
			return errors.New("--target-app can be used only with migrate wodby1 instance")
		}
		resolvedApp, resolvedStackID, err := targetClient.ResolveInstanceTargetApp(cmd.Context(), &scope, selector, targetStackID)
		if err != nil {
			return err
		}
		targetStackID = resolvedStackID
		targetApp = &resolvedApp
		preparation.CompleteStep(fmt.Sprintf("Existing target app %s (ID %d) will receive the new app environment using stack ID %d.", resolvedApp.Name, resolvedApp.ID, targetStackID))
	}

	// The customer command transfers required credentials in memory. Wodby 1
	// independently authorizes this export using the source organization role.
	var exportSource func(context.Context, string, SourceBackupSelection) (Export, error)
	switch sourceKind {
	case "app":
		exportSource = sourceClient.ExportAppWithBackups
	case "instance":
		exportSource = sourceClient.ExportInstanceWithBackups
	default:
		return errors.Errorf("unsupported single migration source kind %q", sourceKind)
	}
	backupSelection := SourceBackupSelection{}
	if reviewedPlan != nil {
		backupSelection, err = PlanSourceBackups(*reviewedPlan)
		if err != nil {
			return err
		}
	}
	preparation.StartStep("Export and select Wodby 1 source data")
	export, err := exportSource(cmd.Context(), sourceID, backupSelection)
	if err != nil {
		return err
	}
	if reviewedPlan == nil && len(opts.SourceBackups) != 0 {
		backupSelection, err = ResolveSourceBackups(export, sourceKind, opts.SourceBackups)
		if err != nil {
			return err
		}
		export, err = exportSource(cmd.Context(), sourceID, backupSelection)
		if err != nil {
			return err
		}
	} else if reviewedPlan != nil && len(opts.SourceBackups) != 0 {
		requested, err := ResolveSourceBackups(export, sourceKind, opts.SourceBackups)
		if err != nil {
			return err
		}
		if !sameSourceBackupSelection(requested, backupSelection) {
			return errors.New("--source-backup does not match the snapshots pinned by the applied plan; use the original selection or restart before target changes")
		}
	}
	export, selection, err := SelectExport(export, sourceKind, opts.ExcludeApps, opts.ExcludeInstances, opts.SourceToken)
	if err != nil {
		return err
	}
	workspaceInstances, err := resolveWorkspaceInstances(export, sourceKind, opts)
	if err != nil {
		return err
	}
	backupSelection, err = ExportSourceBackups(export)
	if err != nil {
		return err
	}
	preparation.CompleteStep(migrationExportSummary(export))

	preparation.StartStep("Resolve target environment types")
	selectors, err := TargetEnvironmentSelectors(export, envMap)
	if err != nil {
		return err
	}
	resolved, err := targetClient.ResolveTargetEnvs(cmd.Context(), scope.Org.ID, selectors)
	if err != nil {
		return err
	}
	targetEnvs := make(map[string]TargetEnv, len(resolved))
	for _, item := range resolved {
		targetEnvs[item.Selector] = item.Env
	}
	preparation.CompleteStep(fmt.Sprintf("Resolved %d target environment selector(s).", len(resolved)))

	preparation.StartStep("Build migration plan")
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:                    sourceKind,
		SourceID:                      sourceID,
		TargetOrg:                     scope.Org.Name,
		TargetProject:                 opts.TargetProject,
		TargetCluster:                 opts.TargetCluster,
		TargetEnvMap:                  envMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackID:                 targetStackID,
		TargetApp:                     targetApp,
		TargetStackMap:                stackMap,
		TargetServiceMap:              serviceMap,
		TargetVersionMap:              versionMap,
		TargetImportMap:               importMap,
		TargetCIIntegrationID:         opts.TargetCIIntegrationID,
		TargetWodbyCI:                 opts.TargetWodbyCI,
		TargetLetsEncrypt:             opts.TargetLetsEncrypt,
		Repository: RepositoryTargetPlan{
			GitIntegrationID: opts.TargetGitIntegrationID,
			RepositoryName:   strings.TrimSpace(opts.TargetRepositoryName),
			Service:          strings.TrimSpace(opts.TargetCodeService),
		},
		SkipCode:                opts.SkipCode,
		SkipData:                opts.SkipData,
		RequireData:             !opts.SkipData,
		AllowUnsupportedDrupal:  opts.AllowUnsupportedDrupal,
		Selection:               &selection,
		WorkspaceInstances:      workspaceInstances,
		WorkspaceStorageClass:   opts.WorkspaceStorageClass,
		WorkspaceStorageService: opts.WorkspaceStorageService,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Planned %d app(s) and %d instance(s).", plan.Summary.Apps, plan.Summary.Instances))
	preparation.StartStep("Inspect target mappings and capabilities")
	preflightState := resumeState
	if restartStateWithMutations != nil {
		preflightState = restartStateWithMutations
	}
	if PlanNeedsTargetStack(plan) {
		preparation.CompleteStep("Skipped: a custom stack needs a target stack first.")
		cmd.onPlan(plan, nil)
		return showUnpreparedPlan(cmd, plan, opts.Apply, opts.Verify)
	}
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, TargetPreflightOptions{
		SkipCode:                    opts.SkipCode,
		SkipData:                    opts.SkipData,
		GitRef:                      opts.TargetGitRef,
		GitRefType:                  opts.TargetGitRefType,
		CodeService:                 opts.TargetCodeService,
		AllowedTargetAppID:          allowedTargetAppID,
		AllowStateBackedAppRecovery: allowTargetAppRecovery,
		AllowedTargetInstanceIDs:    stateTargetInstanceIDs(preflightState),
		StateBackedInstanceRecovery: stateTargetInstanceRecovery(preflightState),
		AddMissingServices:          opts.AddMissingServices,
		Progress:                    preparation.TargetPreflight,
	})
	if err != nil {
		return err
	}
	preparation.CompleteStep("Target preflight completed.")

	cmd.onPlan(plan, &prepared)
	if !opts.Apply && !opts.Verify {
		return printPreview(cmd, plan, prepared)
	}
	if plan.Summary.Blocking != 0 {
		action := "apply"
		if opts.Verify {
			action = "verify"
		}
		return rejectBlockedMigrationAction(cmd, plan, action, prepared)
	}
	if reviewedPlan != nil {
		if err := RestoreReviewedPlanForResume(&plan, *reviewedPlan); err != nil {
			return errors.Wrapf(
				err,
				"cannot continue from saved plan because the current source or options change its executable actions; the saved plan was not overwritten\nNext step: restart with this command (the source token is intentionally omitted):\n  %s\nThe CLI will replace the saved plan and safely remove only resources this migration created",
				cmd.restartSuggestion(sourceKind, sourceID),
			)
		}
	}
	if err := artifacts.Prepare(); err != nil {
		return err
	}
	stateLock, err := artifacts.Lock(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := stateLock.Close(); runErr == nil && err != nil {
			runErr = err
		}
	}()
	if opts.Apply {
		var restartCleanup *migrationRestartCleanup
		if resumeState == nil && opts.Restart {
			cleanupAppName := restartStateAppName
			if cleanupAppName == "" {
				cleanupAppName = sourceID
			}
			restartCleanup = &migrationRestartCleanup{StatePath: statePath, State: restartStateWithMutations, AppName: cleanupAppName}
			if restartStateWithMutations != nil {
				restartCleanup.Plan, err = PlanRollback(restartStateWithMutations)
				if err != nil {
					return errors.Wrap(err, "prepare --restart cleanup")
				}
			}
		}
		// A plan approved elsewhere must be the one this run applies.
		if err := cmd.checkApprovedPlan(plan); err != nil {
			return err
		}
		if err := printApplyReview(cmd, plan, resumeState != nil, opts.Restart, prepared); err != nil {
			return err
		}
		if resumeState == nil && opts.Restart {
			printRestartConflict(cmd, sourceKind, sourceID, []migrationRestartCleanup{*restartCleanup}, restartStateIdentity != nil, true, planPath, statePath)
		}
		if resumeState == nil {
			if opts.Restart {
				if err := confirmRestart(cmd, opts.Yes, restartCleanup.AppName, restartCleanup.Plan.DeletesResources()); err != nil {
					return err
				}
			} else {
				if err := confirmApply(cmd, opts.Yes); err != nil {
					return err
				}
			}
		}
		if restartStateWithMutations != nil {
			if err := runMigrationRestartCleanupLocked(cmd, opts, targetClient, artifacts, *restartCleanup); err != nil {
				return err
			}
		}
		if restartStateIdentity != nil {
			if err := artifacts.RemoveRestartableState(statePath, *restartStateIdentity); err != nil {
				return errors.Wrap(err, "replace migration state for restart")
			}
		}
		if resumeState == nil {
			if err := artifacts.SavePlan(plan); err != nil {
				return err
			}
			printArtifactNotice(cmd, planPath, statePath)
		}
	}

	executor, err := NewMigrationExecutor(targetClient, MigrationExecutorOptions{
		State:                   artifacts.StateStore(statePath),
		PollInterval:            opts.PollInterval,
		OperationTimeout:        opts.WaitTimeout,
		RetryAmbiguousOperation: opts.RetryAmbiguous,
		Progress:                migrationProgressReporter(cmd),
		RefreshSource: func(ctx context.Context) (Export, error) {
			refreshed, err := exportSource(ctx, sourceID, backupSelection)
			if err != nil {
				return Export{}, err
			}
			return ApplySourceSelection(refreshed, plan.Selection, opts.SourceToken)
		},
	})
	if err != nil {
		return err
	}

	var action string
	if opts.Apply {
		action = "apply"
		_, err = executor.Apply(cmd.Context(), export, plan, prepared)
	} else {
		action = "verify"
		_, err = executor.Verify(cmd.Context(), export, plan, prepared, scope.Cluster)
	}
	if err != nil {
		if opts.Apply {
			if paused, ok := AsMigrationPaused(err); ok {
				return printMigrationPaused(cmd, paused, planPath, statePath)
			}
			return errors.Wrapf(
				err,
				"migration apply stopped; inspect the target and resume with the same --apply command (state: %s)",
				statePath,
			)
		}
		return errors.Wrap(err, "migration verification failed")
	}
	return printMigrationResult(cmd, action, plan, planPath, statePath)
}

func migrationProgressReporter(cmd *RunEnv) func(string) {
	w := migrationProgressWriter(cmd)
	step := 0
	return func(message string) {
		message = strings.TrimSpace(message)
		if message == "" {
			return
		}
		switch message {
		case "Starting resumable migration apply.":
			fmt.Fprintln(w, "\n"+cliColor(w, cliColorBold+cliColorCyan, "Migration process"))
			return
		case "Starting migration verification.":
			fmt.Fprintln(w, "\n"+cliColor(w, cliColorBold+cliColorCyan, "Verification process"))
			return
		}
		if title, ok := migrationProgressStepTitle(message); ok {
			step++
			fmt.Fprintf(w, "\n%s\n", cliColor(w, cliColorBold+cliColorCyan, fmt.Sprintf("Step %d: %s", step, title)))
			return
		}
		fmt.Fprintf(w, "  %s\n", cliColor(w, progressMessageColor(message), message))
	}
}

const migrationPreparationSteps = 5

type migrationPreparationProgress struct {
	w       io.Writer
	enabled bool
	kind    string
	step    int
}

func newMigrationPreparationProgress(cmd *RunEnv, sourceKind string) *migrationPreparationProgress {
	sourceKind = strings.ToLower(strings.TrimSpace(sourceKind))
	return &migrationPreparationProgress{
		w:       migrationProgressWriter(cmd),
		enabled: sourceKind == "app" || sourceKind == "server",
		kind:    sourceKind,
	}
}

func (p *migrationPreparationProgress) StartStep(title string) {
	if p == nil || !p.enabled {
		return
	}
	if p.step == 0 {
		title := p.kind
		if title != "" {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
		fmt.Fprintf(p.w, "\n%s\n", cliColor(p.w, cliColorBold+cliColorCyan, title+" migration preparation"))
	}
	p.step++
	fmt.Fprintf(p.w, "\n%s\n", cliColor(p.w, cliColorBold+cliColorCyan, fmt.Sprintf("[%d/%d] %s", p.step, migrationPreparationSteps, title)))
}

func (p *migrationPreparationProgress) CompleteStep(message string) {
	if p == nil || !p.enabled || strings.TrimSpace(message) == "" {
		return
	}
	fmt.Fprintf(p.w, "  %s\n", cliColor(p.w, cliColorGreen, strings.TrimSpace(message)))
}

func (p *migrationPreparationProgress) TargetPreflight(event TargetPreflightProgress) {
	if p == nil || !p.enabled {
		return
	}
	switch event.Stage {
	case "app":
		fmt.Fprintf(
			p.w,
			"  %s\n",
			cliColor(p.w, cliColorBold, fmt.Sprintf("App %d/%d: %s (%d instance(s))", event.AppIndex, event.AppTotal, event.AppName, event.InstanceTotal)),
		)
	case "instance":
		fmt.Fprintf(p.w, "    Instance %d/%d: %s\n", event.InstanceIndex, event.InstanceTotal, event.InstanceName)
	case "app_complete":
		fmt.Fprintf(p.w, "    %s\n", cliColor(p.w, cliColorGreen, fmt.Sprintf("App %d/%d inspected.", event.AppIndex, event.AppTotal)))
	}
}

func migrationProgressWriter(cmd *RunEnv) io.Writer {
	if planOutputJSON(cmd) {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

func migrationExportSummary(export Export) string {
	apps := export.AppExports()
	instances := 0
	for _, app := range apps {
		instances += len(app.Instances)
	}
	return fmt.Sprintf("Selected %d app(s) and %d instance(s) from Wodby 1.", len(apps), instances)
}

func migrationProgressStepTitle(message string) (string, bool) {
	for _, prefix := range []string{"Step: ", "Preflight: "} {
		if strings.HasPrefix(message, prefix) {
			return strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(message, prefix)), "."), true
		}
	}
	return "", false
}

func runWodby1Server(cmd *RunEnv, sourceID string, opts *RunOptions) (runErr error) {
	opts.SourceToken = firstNonBlank(opts.SourceToken, "")
	if err := validateOptions(opts); err != nil {
		return err
	}
	var err error
	opts.SourceToken, err = NormalizeSourceToken(opts.SourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.TargetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseTargetStackMapping(opts.TargetStackMap)
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.TargetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	versionMap, err := parseMapping(opts.TargetVersionMap, "--target-version-map")
	if err != nil {
		return err
	}
	importMap, err := parseMapping(opts.TargetImportMap, "--target-import-map")
	if err != nil {
		return err
	}
	repositoryMap, err := parseRepositoryMapping(opts.TargetRepositoryMap)
	if err != nil {
		return err
	}
	sourceClient, err := NewSourceClient(opts.SourceBaseURL, opts.SourceToken)
	if err != nil {
		return err
	}

	artifacts, err := cmd.artifacts("server", sourceID, opts.StateFile)
	if err != nil {
		return err
	}
	planPath, statePath := artifacts.PlanLocation(), artifacts.StateLocation()
	// Local command and flag validation is complete. Keep Cobra's usage output
	// for errors above this boundary, but suppress it for migration failures.
	cmd.silenceUsage()

	var reviewedPlan *Plan
	var restartStateIdentities map[string]MigrationStateIdentity
	restartCleanupStates := map[string]*MigrationState{}
	var incompatiblePlan *UnsupportedMigrationPlanSchemaError
	var incompatibleServerStates map[string]*MigrationState
	statePaths, err := artifacts.ServerStates()
	if err != nil {
		return err
	}
	stateExists := len(statePaths) != 0
	planExists, err := artifacts.Exists(planPath)
	if err != nil {
		return err
	}
	if opts.Rollback {
		if !stateExists {
			return errors.Errorf("no server migration state found for %s; there is nothing this migration created to roll back", sourceID)
		}
		return runServerMigrationRollback(cmd, opts, artifacts, sourceID, planPath, statePaths)
	}
	if opts.Verify && !stateExists {
		return errors.Errorf("no applied server migration state found for %s; run the same command with --apply first", sourceID)
	}
	if opts.Restart && !stateExists {
		return errors.Errorf("--restart found no applied server migration state for %s; run --apply without --restart to start a new migration", sourceID)
	}
	if (opts.Apply || opts.Verify) && stateExists {
		if !planExists {
			return errors.Errorf(
				"applied server migration state exists for %s but its plan is missing at %s; restore the plan before resuming",
				sourceID,
				planPath,
			)
		}
		reviewed, err := artifacts.LoadPlan()
		if err != nil {
			if !errors.As(err, &incompatiblePlan) {
				return errors.Wrap(err, "load applied migration plan")
			}
			if incompatiblePlan.SourceKind != "server" || incompatiblePlan.SourceID != sourceID {
				return errors.New("incompatible applied migration plan does not match the requested source server")
			}
			incompatibleServerStates, err = inspectIncompatibleServerMigrationStates(artifacts, statePaths)
			if err != nil {
				return err
			}
			states := make([]*MigrationState, 0, len(incompatibleServerStates))
			for _, path := range statePaths {
				states = append(states, incompatibleServerStates[path])
			}
			if !opts.Restart {
				return incompatiblePlanRestartError(incompatiblePlan, planPath, statePaths, states)
			}
			restartStateIdentities = make(map[string]MigrationStateIdentity, len(incompatibleServerStates))
			for _, path := range statePaths {
				state := incompatibleServerStates[path]
				if state.CanRestartSafely() {
					restartStateIdentities[path] = state.Identity()
					continue
				}
				if _, err := PlanRollback(state); err != nil {
					return errors.Wrapf(err, "prepare --restart cleanup for source app %s", state.Source.ID)
				}
				restartCleanupStates[path] = state
			}
		} else {
			if reviewed.Source.Kind != "server" || reviewed.Source.ID != sourceID {
				return errors.New("applied migration plan does not match the requested source server")
			}
			if !opts.Restart && (reviewed.Status == "blocked" || reviewed.Summary.Blocking != 0) {
				return errors.Errorf(
					"applied migration plan is blocked by %d review item(s); inspect %s",
					reviewed.Summary.Blocking,
					planPath,
				)
			}
			if opts.Restart {
				identities, cleanup, err := serverMigrationStatesForRestart(artifacts, reviewed, statePaths)
				if err != nil {
					return err
				}
				restartStateIdentities = identities
				restartCleanupStates = cleanup
			} else {
				reviewedPlan = &reviewed
				if opts.Apply {
					identities, restartable, err := restartableServerMigrationStates(artifacts, reviewed, statePaths)
					if err != nil {
						return err
					}
					if restartable {
						restartStateIdentities = identities
					}
				}
			}
		}
	}
	if opts.Apply && reviewedPlan != nil {
		printServerResumeNotice(cmd, planPath, statePath, len(statePaths))
	}

	targetClient, err := cmd.targetClient()
	if err != nil {
		return err
	}
	preparation := newMigrationPreparationProgress(cmd, "server")
	preparation.StartStep("Discover Wodby 2 target scope")
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), TargetScopeSelectors{
		Project: opts.TargetProject,
		Cluster: opts.TargetCluster,
	})
	if err != nil {
		return err
	}
	for _, state := range restartCleanupStates {
		if scope.Org.ID != state.Target.OrgID || scope.Cluster.ID != state.Target.ClusterID {
			return errors.Errorf(
				"cannot restart the saved server migration because the current target resolves to organization ID %d and cluster ID %d instead of the saved organization ID %d and cluster ID %d; use the original target credentials and options",
				scope.Org.ID,
				scope.Cluster.ID,
				state.Target.OrgID,
				state.Target.ClusterID,
			)
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Target %s / %s is ready.", scope.Org.Name, scope.Cluster.Name))

	backupSelection := SourceBackupSelection{}
	if reviewedPlan != nil {
		backupSelection, err = PlanSourceBackups(*reviewedPlan)
		if err != nil {
			return err
		}
	}
	preparation.StartStep("Export and select Wodby 1 source data")
	export, err := sourceClient.ExportServerWithBackups(cmd.Context(), sourceID, backupSelection)
	if err != nil {
		return err
	}
	if reviewedPlan == nil && len(opts.SourceBackups) != 0 {
		backupSelection, err = ResolveSourceBackups(export, "server", opts.SourceBackups)
		if err != nil {
			return err
		}
		export, err = sourceClient.ExportServerWithBackups(cmd.Context(), sourceID, backupSelection)
		if err != nil {
			return err
		}
	} else if reviewedPlan != nil && len(opts.SourceBackups) != 0 {
		requested, err := ResolveSourceBackups(export, "server", opts.SourceBackups)
		if err != nil {
			return err
		}
		if !sameSourceBackupSelection(requested, backupSelection) {
			return errors.New("--source-backup does not match the snapshots pinned by the applied plan; use the original selection or restart before target changes")
		}
	}
	export, selection, err := SelectExport(export, "server", opts.ExcludeApps, opts.ExcludeInstances, opts.SourceToken)
	if err != nil {
		return err
	}
	workspaceInstances, err := resolveWorkspaceInstances(export, "server", opts)
	if err != nil {
		return err
	}
	backupSelection, err = ExportSourceBackups(export)
	if err != nil {
		return err
	}
	if err := validateServerRepositoryOptions(export, opts); err != nil {
		return err
	}
	if err := artifacts.ValidateServerApps(export); err != nil {
		return err
	}
	preparation.CompleteStep(migrationExportSummary(export))

	preparation.StartStep("Resolve target environment types")
	selectors, err := TargetEnvironmentSelectors(export, envMap)
	if err != nil {
		return err
	}
	resolved, err := targetClient.ResolveTargetEnvs(cmd.Context(), scope.Org.ID, selectors)
	if err != nil {
		return err
	}
	targetEnvs := make(map[string]TargetEnv, len(resolved))
	for _, item := range resolved {
		targetEnvs[item.Selector] = item.Env
	}
	preparation.CompleteStep(fmt.Sprintf("Resolved %d target environment selector(s).", len(resolved)))

	preparation.StartStep("Build migration plan")
	plan, err := BuildPlan(export, PlanOptions{
		SourceKind:                    "server",
		SourceID:                      sourceID,
		TargetOrg:                     scope.Org.Name,
		TargetProject:                 opts.TargetProject,
		TargetCluster:                 opts.TargetCluster,
		TargetEnvMap:                  envMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackID:                 opts.TargetStackID,
		TargetStackMap:                stackMap,
		TargetServiceMap:              serviceMap,
		TargetVersionMap:              versionMap,
		TargetImportMap:               importMap,
		TargetCIIntegrationID:         opts.TargetCIIntegrationID,
		TargetWodbyCI:                 opts.TargetWodbyCI,
		TargetLetsEncrypt:             opts.TargetLetsEncrypt,
		Repository: RepositoryTargetPlan{
			GitIntegrationID: opts.TargetGitIntegrationID,
			RepositoryName:   strings.TrimSpace(opts.TargetRepositoryName),
			Service:          strings.TrimSpace(opts.TargetCodeService),
		},
		RepositoryByApp:         repositoryMap,
		SkipCode:                opts.SkipCode,
		SkipData:                opts.SkipData,
		RequireData:             !opts.SkipData,
		AllowUnsupportedDrupal:  opts.AllowUnsupportedDrupal,
		Selection:               &selection,
		WorkspaceInstances:      workspaceInstances,
		WorkspaceStorageClass:   opts.WorkspaceStorageClass,
		WorkspaceStorageService: opts.WorkspaceStorageService,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Planned %d app(s) and %d instance(s).", plan.Summary.Apps, plan.Summary.Instances))

	allowedTargetAppIDs := map[string]int{}
	stateBackedRecovery := map[string]bool{}
	if reviewedPlan != nil {
		for _, app := range reviewedPlan.Apps {
			_, childPlan, _, err := ScopeServerMigrationApp(
				export,
				*reviewedPlan,
				PreparedMigration{},
				app.SourceUUID,
				opts.SourceToken,
			)
			if err != nil {
				return err
			}
			childStatePath := artifacts.ServerAppState(app.SourceUUID)
			if opts.Verify {
				exists, err := artifacts.Exists(childStatePath)
				if err != nil {
					return err
				}
				if !exists {
					return errors.Errorf(
						"no applied migration state found for source app %s (%s) at %s; run the same server command with --apply first",
						app.Name,
						app.SourceUUID,
						childStatePath,
					)
				}
			}
			targetID, allowRecovery, err := stateBackedTargetApp(artifacts, childStatePath, childPlan)
			if err != nil {
				return err
			}
			allowedTargetAppIDs[app.SourceUUID] = targetID
			stateBackedRecovery[app.SourceUUID] = allowRecovery
		}
	}
	if reviewedPlan == nil && len(restartCleanupStates) != 0 {
		for _, state := range restartCleanupStates {
			targetID, allowRecovery := stateBackedTargetAppState(state)
			allowedTargetAppIDs[state.Source.ID] = targetID
			stateBackedRecovery[state.Source.ID] = allowRecovery
		}
	}
	preparation.StartStep("Inspect target mappings and capabilities")
	if PlanNeedsTargetStack(plan) {
		preparation.CompleteStep("Skipped: a custom stack needs a target stack first.")
		cmd.onPlan(plan, nil)
		return showUnpreparedPlan(cmd, plan, opts.Apply, opts.Verify)
	}
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, TargetPreflightOptions{
		SkipCode:               opts.SkipCode,
		SkipData:               opts.SkipData,
		GitRef:                 opts.TargetGitRef,
		GitRefType:             opts.TargetGitRefType,
		CodeService:            opts.TargetCodeService,
		AllowedTargetAppIDs:    allowedTargetAppIDs,
		StateBackedAppRecovery: stateBackedRecovery,
		AddMissingServices:     opts.AddMissingServices,
		Progress:               preparation.TargetPreflight,
	})
	if err != nil {
		return err
	}
	preparation.CompleteStep("Target preflight completed.")

	cmd.onPlan(plan, &prepared)
	if !opts.Apply && !opts.Verify {
		return printPreview(cmd, plan, prepared)
	}
	if plan.Summary.Blocking != 0 {
		action := "apply"
		if opts.Verify {
			action = "verify"
		}
		return rejectBlockedMigrationAction(cmd, plan, action, prepared)
	}
	if reviewedPlan != nil {
		if err := RestoreReviewedPlanForResume(&plan, *reviewedPlan); err != nil {
			return errors.Wrapf(
				err,
				"cannot continue from saved server plan because the current source or options change its executable actions; the saved plan was not overwritten\nNext step: restart with this command (the source token is intentionally omitted):\n  %s\nThe CLI will replace the saved plan and safely remove only resources these migrations created",
				cmd.restartSuggestion("server", sourceID),
			)
		}
	}
	if err := artifacts.Prepare(); err != nil {
		return err
	}
	stateLock, err := artifacts.Lock(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := stateLock.Close(); runErr == nil && err != nil {
			runErr = err
		}
	}()
	if opts.Apply {
		var restartCleanups []migrationRestartCleanup
		if reviewedPlan == nil && opts.Restart {
			appNames := make(map[string]string, len(plan.Apps))
			for _, app := range plan.Apps {
				appNames[app.SourceUUID] = app.Name
			}
			for _, path := range statePaths {
				state := restartCleanupStates[path]
				if state == nil {
					continue
				}
				appName := appNames[state.Source.ID]
				if appName == "" {
					appName = state.Source.ID
				}
				rollback, err := PlanRollback(state)
				if err != nil {
					return errors.Wrapf(err, "prepare --restart cleanup for source app %s", state.Source.ID)
				}
				restartCleanups = append(restartCleanups, migrationRestartCleanup{
					StatePath: path,
					State:     state,
					Plan:      rollback,
					AppName:   appName,
				})
			}
		}
		// A plan approved elsewhere must be the one this run applies.
		if err := cmd.checkApprovedPlan(plan); err != nil {
			return err
		}
		if err := printApplyReview(cmd, plan, reviewedPlan != nil, opts.Restart, prepared); err != nil {
			return err
		}
		if reviewedPlan == nil && opts.Restart {
			printRestartConflict(cmd, "server", sourceID, restartCleanups, len(restartStateIdentities) != 0, false, planPath, statePath)
		}
		if reviewedPlan == nil {
			if opts.Restart {
				destructive := false
				for _, cleanup := range restartCleanups {
					destructive = destructive || cleanup.Plan.DeletesResources()
				}
				if err := confirmRestart(cmd, opts.Yes, "restart "+sourceID, destructive); err != nil {
					return err
				}
			} else {
				if err := confirmApply(cmd, opts.Yes); err != nil {
					return err
				}
			}
		}
		if len(restartCleanups) != 0 {
			for _, cleanup := range restartCleanups {
				if err := runMigrationRestartCleanupLocked(cmd, opts, targetClient, artifacts, cleanup); err != nil {
					return errors.Wrapf(err, "restart cleanup for source app %s", cleanup.State.Source.ID)
				}
			}
		}
		if len(restartStateIdentities) != 0 {
			if err := removeServerMigrationStates(artifacts, restartStateIdentities, nil); err != nil {
				return errors.Wrap(err, "replace safely restartable server migration states")
			}
		}
		if reviewedPlan == nil {
			if err := artifacts.SavePlan(plan); err != nil {
				return err
			}
			printServerArtifactNotice(cmd, planPath, statePath)
		}
	}

	executions := make([]serverAppExecution, 0, len(prepared.Apps))
	for _, preparedApp := range prepared.Apps {
		appUUID := preparedApp.App.App.UUID
		childExport, childPlan, childPrepared, err := ScopeServerMigrationApp(
			export,
			plan,
			prepared,
			appUUID,
			opts.SourceToken,
		)
		if err != nil {
			return err
		}
		childStatePath := artifacts.ServerAppState(appUUID)
		executor, err := NewMigrationExecutor(targetClient, MigrationExecutorOptions{
			State:                   artifacts.StateStore(childStatePath),
			PollInterval:            opts.PollInterval,
			OperationTimeout:        opts.WaitTimeout,
			RetryAmbiguousOperation: opts.RetryAmbiguous,
			Progress:                migrationProgressReporter(cmd),
			RefreshSource: func(ctx context.Context) (Export, error) {
				refreshed, err := sourceClient.ExportServerWithBackups(ctx, sourceID, backupSelection)
				if err != nil {
					return Export{}, err
				}
				refreshed, err = ApplySourceSelection(refreshed, plan.Selection, opts.SourceToken)
				if err != nil {
					return Export{}, err
				}
				return ScopeServerExportApp(refreshed, appUUID, opts.SourceToken)
			},
		})
		if err != nil {
			return err
		}
		executions = append(executions, serverAppExecution{
			sourceAppUUID: appUUID,
			name:          preparedApp.App.App.Name,
			statePath:     childStatePath,
			export:        childExport,
			plan:          childPlan,
			prepared:      childPrepared,
			executor:      executor,
		})
	}
	results := make([]serverAppMigrationOutput, 0, len(executions))
	failedApps := 0
	pausedActions := []*ExternalActionRequiredError{}
	serverAction := "verification"
	if opts.Apply {
		serverAction = "migration"
	}
	progressWriter := migrationProgressWriter(cmd)
	for executionIndex, execution := range executions {
		fmt.Fprintf(
			progressWriter,
			"\n%s\n",
			cliColor(progressWriter, cliColorBold+cliColorCyan, fmt.Sprintf("Server %s: app %d/%d: %s", serverAction, executionIndex+1, len(executions), execution.name)),
		)
		if opts.Apply {
			_, err = execution.executor.Apply(cmd.Context(), execution.export, execution.plan, execution.prepared)
		} else {
			_, err = execution.executor.Verify(cmd.Context(), execution.export, execution.plan, execution.prepared, scope.Cluster)
		}
		if err != nil {
			// A paused app is waiting on its own CI pipeline, not broken, so it
			// must not be reported or counted as a failed app.
			if paused, ok := AsMigrationPaused(err); ok {
				pausedActions = append(pausedActions, paused.Actions...)
				results = append(results, serverAppMigrationOutput{
					SourceAppUUID: execution.sourceAppUUID,
					Name:          execution.name,
					StateFile:     execution.statePath,
					Status:        pausedStatus(paused),
					Error:         paused.Error(),
				})
				fmt.Fprintf(
					progressWriter,
					"%s\n%s",
					cliColor(progressWriter, cliColorOrange, fmt.Sprintf("App %d/%d paused: %s. Continuing with the next app.", executionIndex+1, len(executions), execution.name)),
					paused.NextSteps(),
				)
				continue
			}
			failedApps++
			results = append(results, serverAppMigrationOutput{
				SourceAppUUID: execution.sourceAppUUID,
				Name:          execution.name,
				StateFile:     execution.statePath,
				Status:        "failed; rerun to resume",
				Error:         err.Error(),
			})
			fmt.Fprintf(
				progressWriter,
				"%s\n",
				cliColor(progressWriter, cliColorRed, fmt.Sprintf("App %d/%d failed: %s. Continuing with the next app.", executionIndex+1, len(executions), execution.name)),
			)
			continue
		}
		results = append(results, serverAppMigrationOutput{
			SourceAppUUID: execution.sourceAppUUID,
			Name:          execution.name,
			StateFile:     execution.statePath,
			Status:        publicMigrationStatus(opts.Apply),
		})
		fmt.Fprintf(
			progressWriter,
			"%s\n",
			cliColor(progressWriter, cliColorGreen, fmt.Sprintf("App %d/%d completed: %s", executionIndex+1, len(executions), execution.name)),
		)
	}
	action := "verify"
	if opts.Apply {
		action = "apply"
	}
	if err := printServerMigrationResult(cmd, action, plan, planPath, results); err != nil {
		return err
	}
	if failedApps != 0 {
		return errors.Errorf("server migration completed with %d failed app(s); inspect the result above and rerun the same command to resume", failedApps)
	}
	if len(pausedActions) != 0 {
		cmd.silenceUsage()
		// Wrap rather than replace, so this exits with the paused status like a
		// single-app run does instead of looking like a failure to automation.
		return errors.Wrapf(
			&MigrationPausedError{Actions: pausedActions},
			"server migration is paused on %d instance(s) waiting for their first Custom CI build;"+
				" follow the steps above, then rerun the same command to resume",
			len(pausedActions),
		)
	}
	return nil
}

func validateOptions(opts *RunOptions) error {
	if strings.TrimSpace(opts.SourceBaseURL) == "" {
		return errors.New("--source-base-url is required")
	}
	if _, err := url.ParseRequestURI(opts.SourceBaseURL); err != nil {
		return errors.Wrap(err, "invalid --source-base-url")
	}
	if strings.TrimSpace(opts.SourceToken) == "" {
		return errors.Errorf("--source-token or %s is required", SourceTokenEnv)
	}
	if opts.Output != "text" && opts.Output != "json" {
		return errors.Errorf("unsupported --output %q", opts.Output)
	}
	if opts.Apply && opts.Verify {
		return errors.New("--apply and --verify cannot be used together")
	}
	if opts.Restart && !opts.Apply {
		return errors.New("--restart requires --apply")
	}
	if opts.Rollback && (opts.Apply || opts.Verify || opts.Restart) {
		return errors.New("--rollback cannot be combined with --apply, --verify, or --restart")
	}
	if len(opts.SourceBackups) != 0 && opts.SkipData {
		return errors.New("--source-backup cannot be used with --skip-data")
	}
	if strings.TrimSpace(opts.TargetCluster) == "" {
		return errors.New("--target-cluster is required")
	}
	if opts.PollInterval <= 0 {
		return errors.New("--poll-interval must be positive")
	}
	if opts.WaitTimeout <= 0 {
		return errors.New("--wait-timeout must be positive")
	}
	if opts.TargetCIIntegrationID < 0 {
		return errors.New("--target-ci-integration-id cannot be negative")
	}
	if opts.TargetWodbyCI && opts.TargetCIIntegrationID > 0 {
		return errors.New("--target-wodby-ci and --target-ci-integration-id cannot be used together")
	}
	if opts.TargetGitIntegrationID < 0 {
		return errors.New("--target-git-integration-id cannot be negative")
	}
	if opts.TargetStackID < 0 {
		return errors.New("--target-stack-id must be a positive ID")
	}
	refType := strings.ToLower(strings.TrimSpace(opts.TargetGitRefType))
	if refType != "" && refType != "branch" && refType != "tag" && refType != "commit" {
		return errors.New("--target-git-ref-type must be branch, tag, or commit")
	}
	if (strings.TrimSpace(opts.TargetGitRef) == "") != (refType == "") {
		return errors.New("--target-git-ref and --target-git-ref-type must be specified together")
	}
	return nil
}

func parseMapping(values []string, flag string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key, target, ok := strings.Cut(part, "=")
			if !ok {
				return nil, fmt.Errorf("%s value %q must be in source=target format", flag, part)
			}
			key = strings.ToLower(strings.TrimSpace(key))
			target = strings.TrimSpace(target)
			if key == "" || target == "" {
				return nil, fmt.Errorf("%s value %q must be in source=target format", flag, part)
			}
			if existing, exists := result[key]; exists && existing != target {
				return nil, fmt.Errorf("%s contains conflicting mappings for source %q", flag, key)
			}
			result[key] = target
		}
	}
	return result, nil
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

// sameSourceBackupSelection reports whether every requested snapshot is the one
// the applied plan pinned. The request lists only the instances the operator
// overrode, while the plan pins every instance that has imports.
func sameSourceBackupSelection(left, right SourceBackupSelection) bool {
	for instanceID, leftComponents := range left {
		rightComponents, found := right[instanceID]
		if !found {
			return false
		}
		if selected := strings.TrimSpace(leftComponents["*"]); selected != "" {
			if len(rightComponents) == 0 {
				return false
			}
			for _, backupUUID := range rightComponents {
				if backupUUID != selected {
					return false
				}
			}
			continue
		}
		if len(leftComponents) != len(rightComponents) {
			return false
		}
		for component, backupUUID := range leftComponents {
			if rightComponents[component] != backupUUID {
				return false
			}
		}
	}
	return true
}

func parseTargetStackMapping(values []string) (map[string]string, error) {
	result, err := parseMapping(values, "--target-stack-map")
	if err != nil {
		return nil, err
	}
	for source, target := range result {
		id, err := strconv.Atoi(target)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--target-stack-map target %q for source %q must be a positive stack ID", target, source)
		}
		result[source] = strconv.Itoa(id)
	}
	return result, nil
}

func parseRepositoryMapping(values []string) (map[string]RepositoryTargetPlan, error) {
	result := map[string]RepositoryTargetPlan{}
	for _, value := range values {
		key, target, ok := strings.Cut(strings.TrimSpace(value), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || key == "" || strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("--target-repository-map value %q must use APP=GIT_INTEGRATION_ID[:REPOSITORY_NAME[:SERVICE]]", value)
		}
		parts := strings.SplitN(target, ":", 3)
		integrationID, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || integrationID <= 0 {
			return nil, fmt.Errorf("--target-repository-map value %q must contain a positive Git integration ID", value)
		}
		mapping := RepositoryTargetPlan{
			GitIntegrationID: integrationID,
		}
		if len(parts) >= 2 {
			mapping.RepositoryName = strings.TrimSpace(parts[1])
			if mapping.RepositoryName == "" {
				return nil, fmt.Errorf("--target-repository-map value %q contains an empty repository name", value)
			}
		}
		if len(parts) == 3 {
			mapping.Service = strings.TrimSpace(parts[2])
			if mapping.Service == "" {
				return nil, fmt.Errorf("--target-repository-map value %q contains an empty target service", value)
			}
		}
		if existing, exists := result[key]; exists && existing != mapping {
			return nil, fmt.Errorf("--target-repository-map contains conflicting mappings for source app %q", key)
		}
		result[key] = mapping
	}
	return result, nil
}

func validateServerRepositoryOptions(export Export, opts *RunOptions) error {
	if opts == nil || opts.SkipCode {
		return nil
	}
	repositories := 0
	for _, app := range export.AppExports() {
		if app.App.Repository != nil {
			repositories++
		}
	}
	if repositories > 1 &&
		strings.TrimSpace(opts.TargetRepositoryName) != "" {
		return errors.New("a server export contains multiple source repositories; use --target-repository-map for per-app repository-name overrides instead of one shared --target-repository-name, or use --skip-code")
	}
	return nil
}

func restartableServerMigrationStates(
	artifacts MigrationArtifacts,
	plan Plan,
	paths []string,
) (map[string]MigrationStateIdentity, bool, error) {
	appIDs := make(map[string]bool, len(plan.Apps))
	for _, app := range plan.Apps {
		appIDs[app.SourceUUID] = true
	}
	identities := make(map[string]MigrationStateIdentity, len(paths))
	for _, path := range paths {
		state, err := artifacts.InspectState(path)
		if err != nil {
			return nil, false, errors.Wrap(err, "inspect server app migration state")
		}
		if state.Source.Kind != "app" || !appIDs[state.Source.ID] {
			return nil, false, errors.Errorf(
				"server migration state %s does not belong to an app in the applied plan",
				path,
			)
		}
		expectedPath := artifacts.ServerAppState(state.Source.ID)
		same, err := artifacts.Same(path, expectedPath)
		if err != nil {
			return nil, false, err
		}
		if !same {
			return nil, false, errors.Errorf(
				"server migration state %s does not match its source app identity",
				path,
			)
		}
		if state.Target.OrgID != plan.Target.OrgID ||
			state.Target.ProjectID != plan.Target.ProjectID ||
			state.Target.ClusterID != plan.Target.ClusterID {
			return nil, false, errors.Errorf(
				"server migration state %s does not match the applied plan target",
				path,
			)
		}
		if !state.CanRestartSafely() {
			return nil, false, nil
		}
		identities[path] = state.Identity()
	}
	return identities, len(identities) != 0, nil
}

func serverMigrationStatesForRestart(
	artifacts MigrationArtifacts,
	plan Plan,
	paths []string,
) (map[string]MigrationStateIdentity, map[string]*MigrationState, error) {
	appIDs := make(map[string]bool, len(plan.Apps))
	for _, app := range plan.Apps {
		appIDs[app.SourceUUID] = true
	}
	safe := make(map[string]MigrationStateIdentity, len(paths))
	cleanup := make(map[string]*MigrationState, len(paths))
	for _, path := range paths {
		state, err := artifacts.InspectState(path)
		if err != nil {
			return nil, nil, errors.Wrap(err, "inspect server app migration state")
		}
		if state.Source.Kind != "app" || !appIDs[state.Source.ID] {
			return nil, nil, errors.Errorf("server migration state %s does not belong to an app in the applied plan", path)
		}
		expectedPath := artifacts.ServerAppState(state.Source.ID)
		same, err := artifacts.Same(path, expectedPath)
		if err != nil {
			return nil, nil, err
		}
		if !same {
			return nil, nil, errors.Errorf("server migration state %s does not match its source app identity", path)
		}
		if state.Target.OrgID != plan.Target.OrgID ||
			state.Target.ProjectID != plan.Target.ProjectID ||
			state.Target.ClusterID != plan.Target.ClusterID {
			return nil, nil, errors.Errorf("server migration state %s does not match the applied plan target", path)
		}
		if state.CanRestartSafely() {
			safe[path] = state.Identity()
			continue
		}
		if _, err := PlanRollback(state); err != nil {
			return nil, nil, errors.Wrapf(err, "prepare --restart cleanup for source app %s", state.Source.ID)
		}
		cleanup[path] = state
	}
	return safe, cleanup, nil
}

func inspectIncompatibleServerMigrationStates(
	artifacts MigrationArtifacts,
	paths []string,
) (map[string]*MigrationState, error) {
	states := make(map[string]*MigrationState, len(paths))
	seenApps := make(map[string]bool, len(paths))
	var target *MigrationStateTarget
	for _, path := range paths {
		state, err := artifacts.InspectState(path)
		if err != nil {
			return nil, errors.Wrap(err, "inspect server app migration state for incompatible plan")
		}
		if state.Source.Kind != "app" || state.Source.ID == "" || seenApps[state.Source.ID] {
			return nil, errors.Errorf("server migration state %s has an invalid source app identity", path)
		}
		seenApps[state.Source.ID] = true
		expectedPath := artifacts.ServerAppState(state.Source.ID)
		same, err := artifacts.Same(path, expectedPath)
		if err != nil {
			return nil, err
		}
		if !same {
			return nil, errors.Errorf("server migration state %s does not match its source app identity", path)
		}
		if target == nil {
			copy := state.Target
			target = &copy
		} else if state.Target != *target {
			return nil, errors.New("server migration states for the incompatible plan do not share one target")
		}
		states[path] = state
	}
	return states, nil
}

func removeServerMigrationStates(
	artifacts MigrationArtifacts,
	expected map[string]MigrationStateIdentity,
	deletedTargetAppIDs map[string]int,
) error {
	paths, err := artifacts.ServerStates()
	if err != nil {
		return err
	}
	if len(paths) != len(expected) {
		return ErrMigrationStateConcurrentUpdate
	}
	for _, path := range paths {
		identity, exists := expected[path]
		if !exists {
			return ErrMigrationStateConcurrentUpdate
		}
		var err error
		if targetAppID := deletedTargetAppIDs[path]; targetAppID > 0 {
			err = artifacts.RemoveStateAfterTargetDeletion(path, identity, targetAppID)
		} else {
			err = artifacts.RemoveRestartableState(path, identity)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func stateBackedTargetApp(
	artifacts MigrationArtifacts,
	statePath string,
	plan Plan,
) (targetID int, allowRecovery bool, err error) {
	state, err := artifacts.LoadState(statePath, migrationStateIdentity(plan))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, errors.Wrap(err, "load migration state before target preflight")
	}
	targetID, allowRecovery = stateBackedTargetAppState(state)
	return targetID, allowRecovery, nil
}

func migrationStateIdentity(plan Plan) MigrationStateIdentity {
	return MigrationStateIdentity{
		Source: MigrationStateSourceIdentity{
			Kind:         plan.Source.Kind,
			ID:           plan.Source.ID,
			ConfigDigest: plan.Source.ConfigDigest,
		},
		PlanHash: plan.PlanHash,
		Target: MigrationStateTarget{
			OrgID:       plan.Target.OrgID,
			ProjectID:   plan.Target.ProjectID,
			ClusterID:   plan.Target.ClusterID,
			AppID:       plan.Target.AppID,
			ExistingApp: plan.Target.AppID > 0,
		},
	}
}

func incompatiblePlanRestartError(
	plan *UnsupportedMigrationPlanSchemaError,
	planPath string,
	statePaths []string,
	states []*MigrationState,
) error {
	var message strings.Builder
	fmt.Fprintf(&message, "the saved migration was created by an incompatible CLI version and cannot be resumed\n")
	fmt.Fprintf(&message, "Saved plan schema: %s\n", plan.Actual)
	fmt.Fprintf(&message, "Supported schema: %s\n", plan.Supported)
	fmt.Fprintf(&message, "Plan: %s\n", planPath)
	if len(statePaths) == 1 {
		fmt.Fprintf(&message, "State: %s", statePaths[0])
	} else {
		fmt.Fprintf(&message, "States:")
		for _, path := range statePaths {
			fmt.Fprintf(&message, "\n  - %s", path)
		}
	}
	targetIDs := make([]string, 0, len(states))
	existingTargetIDs := make([]string, 0, len(states))
	for _, state := range states {
		if state != nil && state.App.TargetID > 0 {
			if state.Target.ExistingApp {
				existingTargetIDs = append(existingTargetIDs, strconv.Itoa(state.Target.AppID))
			} else {
				targetIDs = append(targetIDs, strconv.Itoa(state.App.TargetID))
			}
		}
	}
	if len(existingTargetIDs) != 0 {
		fmt.Fprintf(&message, "\nPre-existing target app ID(s), which restart will preserve: %s", strings.Join(existingTargetIDs, ", "))
	}
	if len(targetIDs) != 0 {
		fmt.Fprintf(&message, "\nSaved target app ID(s): %s", strings.Join(targetIDs, ", "))
	}
	fmt.Fprintf(&message, "\nNext step: rerun the same command with --apply --restart. The CLI will show and confirm cleanup of only migration-created resources, replace the obsolete plan and state, and continue with a fresh apply.")
	return errors.New(message.String())
}

func stateBackedTargetAppState(state *MigrationState) (targetID int, allowRecovery bool) {
	if state.App.TargetID > 0 {
		return state.App.TargetID, false
	}
	operation, found := state.App.Operations["create"]
	if !found {
		return 0, false
	}
	switch operation.Status {
	case MigrationOperationIntent,
		MigrationOperationAccepted,
		MigrationOperationAmbiguous:
		return 0, true
	default:
		return 0, false
	}
}

func stateTargetInstanceIDs(state *MigrationState) map[string]int {
	if state == nil {
		return nil
	}
	result := map[string]int{}
	for sourceID, instance := range state.Instances {
		if instance != nil && instance.TargetID > 0 {
			result[sourceID] = instance.TargetID
		}
	}
	return result
}

func stateTargetInstanceRecovery(state *MigrationState) map[string]bool {
	if state == nil {
		return nil
	}
	result := map[string]bool{}
	for sourceID, instance := range state.Instances {
		if instance == nil || instance.TargetID > 0 {
			continue
		}
		operation, exists := instance.Operations["create"]
		if exists && (operation.Status == MigrationOperationIntent || operation.Status == MigrationOperationAmbiguous) {
			result[sourceID] = true
		}
	}
	return result
}

func printPreview(cmd *RunEnv, plan Plan, prepared ...PreparedMigration) error {
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(plan))
	}
	PrintReview(cmd.OutOrStdout(), plan, prepared...)
	fmt.Fprintln(cmd.OutOrStdout(), "\nNext step:")
	if plan.Status == "blocked" || plan.Summary.Blocking != 0 {
		fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorRed, "Preview result: blocked."))
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"%s\n",
			cliColor(cmd.OutOrStdout(), cliColorRed, fmt.Sprintf("Fix the %d blocking item(s) above, then rerun this preview.", plan.Summary.Blocking)),
		)
		fmt.Fprintln(cmd.OutOrStdout(), "This preview made no changes.")
		printInstanceExclusionHints(cmd, plan)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "No blockers found."))
	warnings := plan.Summary.Confirmation + plan.Summary.ServiceWarnings
	if warnings != 0 {
		fmt.Fprintln(cmd.OutOrStdout(), cliColor(
			cmd.OutOrStdout(),
			cliColorOrange,
			fmt.Sprintf("Review the %d warning(s) above. When ready, rerun the same command with --apply.", warnings),
		))
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "When ready, rerun the same command with --apply.")
	}
	if warnings != 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Adding --apply acknowledges the warnings above; no plan hash is required.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "This preview did not create a plan, state, or lock file.")
	return nil
}

func printApplyReview(cmd *RunEnv, plan Plan, continuing bool, restarting bool, prepared ...PreparedMigration) error {
	if planOutputJSON(cmd) {
		return nil
	}
	PrintReview(cmd.OutOrStdout(), plan, prepared...)
	if continuing {
		fmt.Fprintln(cmd.OutOrStdout(), "\nContinuing the saved migration plan shown above.")
	} else if restarting {
		fmt.Fprintln(cmd.OutOrStdout(), "\nFresh migration plan shown above. It has not started.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "\nApplying the migration plan shown above.")
	}
	return nil
}

// runMigrationRollback deletes the Wodby 2 resources this migration created and
// discards its resume state. It reads the plan only to revalidate the state's
// identity; every deletion target comes from the state itself, so a rollback
// can never remove something the migration did not record creating.
func runMigrationRollback(
	cmd *RunEnv,
	opts *RunOptions,
	artifacts MigrationArtifacts,
	planPath string,
	planExists bool,
	statePath string,
) (runErr error) {
	if !planExists {
		return errors.Errorf(
			"migration state exists at %s but its plan is missing at %s; restore the plan before rolling back",
			statePath,
			planPath,
		)
	}
	reviewed, err := artifacts.LoadPlan()
	if err != nil {
		return err
	}

	// Hold the lock for the whole rollback so a concurrent --apply cannot race
	// deletions against new mutations.
	stateLock, err := artifacts.Lock(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := stateLock.Close(); runErr == nil && err != nil {
			runErr = err
		}
	}()

	state, err := artifacts.LoadState(statePath, migrationStateIdentity(reviewed))
	if err != nil {
		return err
	}
	rollback, err := PlanRollback(state)
	if err != nil {
		return err
	}

	appName := reviewedPlanAppName(reviewed)
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, cliColor(w, cliColorBold+cliColorRed, "\nMigration rollback"))
	fmt.Fprintln(w)
	fmt.Fprint(w, rollback.Describe(appName))
	if err := confirmRollback(cmd, opts.Yes, appName); err != nil {
		return err
	}

	targetClient, err := cmd.targetClient()
	if err != nil {
		return err
	}
	if err := verifyRollbackTarget(cmd.Context(), targetClient, opts, state); err != nil {
		return err
	}
	executor, err := NewMigrationExecutor(targetClient, MigrationExecutorOptions{
		State:            artifacts.StateStore(statePath),
		PollInterval:     opts.PollInterval,
		OperationTimeout: opts.WaitTimeout,
		Progress:         migrationProgressReporter(cmd),
	})
	if err != nil {
		return err
	}
	if err := executor.Rollback(cmd.Context(), state, rollback, appName); err != nil {
		return errors.Wrapf(
			err,
			"migration rollback stopped; inspect Wodby 2 and rerun the same --rollback command to continue (state: %s)",
			statePath,
		)
	}
	fmt.Fprintln(w, cliColor(w, cliColorGreen, "\nRollback completed."))
	fmt.Fprintf(w, "Removed the resume state at %s; Wodby 1 was not touched.\n", statePath)
	return nil
}

// verifyRollbackTarget confirms that the configured API key and cluster are the
// ones the migration was applied to. Rollback deletes by ID and reads a missing
// resource as already deleted, so on another organization or endpoint it would
// delete nothing, report success and discard the only record of what exists.
func verifyRollbackTarget(ctx context.Context, targetClient *TargetClient, opts *RunOptions, state *MigrationState) error {
	scope, err := targetClient.DiscoverTargetScope(ctx, TargetScopeSelectors{
		Project: opts.TargetProject,
		Cluster: opts.TargetCluster,
	})
	if err != nil {
		return errors.Wrap(err, "verify the rollback target")
	}
	if scope.Org.ID != state.Target.OrgID || scope.Cluster.ID != state.Target.ClusterID {
		return errors.Errorf(
			"cannot roll back: the current target resolves to organization ID %d and cluster ID %d, but the migration was applied to organization ID %d and cluster ID %d; use the original API key, API URL and --target-cluster. No Wodby 2 resource was deleted",
			scope.Org.ID, scope.Cluster.ID, state.Target.OrgID, state.Target.ClusterID,
		)
	}
	return nil
}

// runServerMigrationRollback rolls back every app of a server migration. It
// plans all of them first, so one app that cannot be rolled back stops the
// command before anything is deleted.
func runServerMigrationRollback(cmd *RunEnv, opts *RunOptions, artifacts MigrationArtifacts, sourceID, planPath string, statePaths []string) (runErr error) {
	appNames := map[string]string{}
	if reviewed, err := artifacts.LoadPlan(); err == nil {
		for _, app := range reviewed.Apps {
			appNames[app.SourceUUID] = app.Name
		}
	}
	type appRollback struct {
		statePath string
		state     *MigrationState
		plan      RollbackPlan
		name      string
	}
	rollbacks := make([]appRollback, 0, len(statePaths))
	for _, path := range statePaths {
		// Hold every lock until the end so a concurrent --apply cannot race
		// the deletions.
		stateLock, err := artifacts.Lock(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := stateLock.Close(); runErr == nil && err != nil {
				runErr = err
			}
		}()
		state, err := artifacts.InspectState(path)
		if err != nil {
			return errors.Wrap(err, "inspect server app migration state")
		}
		name := appNames[state.Source.ID]
		if name == "" {
			name = state.Source.ID
		}
		plan, err := PlanRollback(state)
		if err != nil {
			return errors.Wrapf(err, "app %s cannot be rolled back; no Wodby 2 resource was deleted", name)
		}
		rollbacks = append(rollbacks, appRollback{statePath: path, state: state, plan: plan, name: name})
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, cliColor(w, cliColorBold+cliColorRed, "\nServer migration rollback"))
	for _, item := range rollbacks {
		fmt.Fprintln(w)
		fmt.Fprint(w, item.plan.Describe(item.name))
	}
	if err := confirmRollback(cmd, opts.Yes, sourceID); err != nil {
		return err
	}
	targetClient, err := cmd.targetClient()
	if err != nil {
		return err
	}
	for _, item := range rollbacks {
		if err := verifyRollbackTarget(cmd.Context(), targetClient, opts, item.state); err != nil {
			return err
		}
	}
	for _, item := range rollbacks {
		executor, err := NewMigrationExecutor(targetClient, MigrationExecutorOptions{
			State:            artifacts.StateStore(item.statePath),
			PollInterval:     opts.PollInterval,
			OperationTimeout: opts.WaitTimeout,
			Progress:         migrationProgressReporter(cmd),
		})
		if err != nil {
			return err
		}
		if err := executor.Rollback(cmd.Context(), item.state, item.plan, item.name); err != nil {
			return errors.Wrapf(
				err,
				"rollback of app %s stopped; inspect Wodby 2 and rerun the same --rollback command to continue (state: %s)",
				item.name, item.statePath,
			)
		}
	}
	fmt.Fprintln(w, cliColor(w, cliColorGreen, "\nRollback completed."))
	fmt.Fprintf(w, "Removed the resume state of %d app(s); Wodby 1 was not touched.\n", len(rollbacks))
	return nil
}

type migrationRestartCleanup struct {
	StatePath string
	State     *MigrationState
	Plan      RollbackPlan
	AppName   string
}

// runMigrationRestartCleanupLocked executes cleanup that was already shown and
// approved as part of the fresh migration plan. The caller holds the migration
// state lock and continues with the new apply after this returns.
func runMigrationRestartCleanupLocked(
	cmd *RunEnv,
	opts *RunOptions,
	targetClient *TargetClient,
	artifacts MigrationArtifacts,
	cleanup migrationRestartCleanup,
) error {
	w := cmd.OutOrStdout()
	headingColor := cliColorBold + cliColorGreen
	if cleanup.Plan.DeletesResources() {
		headingColor = cliColorBold + cliColorRed
	}
	fmt.Fprintln(w, cliColor(w, headingColor, "\nRestart cleanup execution"))

	executor, err := NewMigrationExecutor(targetClient, MigrationExecutorOptions{
		State:            artifacts.StateStore(cleanup.StatePath),
		PollInterval:     opts.PollInterval,
		OperationTimeout: opts.WaitTimeout,
		Progress:         migrationProgressReporter(cmd),
	})
	if err != nil {
		return err
	}
	if err := executor.Rollback(cmd.Context(), cleanup.State, cleanup.Plan, cleanup.AppName); err != nil {
		return errors.Wrap(err, "restart cleanup stopped; inspect Wodby 2 and rerun the same --apply --restart command")
	}
	fmt.Fprintln(w, cliColor(w, cliColorGreen, "Restart cleanup completed. The saved migration will now be replaced."))
	return nil
}

// confirmRollback requires the app name to be typed back. Rollback destroys
// imported data, so a bare y/N is too easy to answer on the wrong terminal.
func confirmRollback(cmd *RunEnv, approved bool, appName string) error {
	// An empty name would turn Enter into approval.
	if strings.TrimSpace(appName) == "" {
		return errors.New("rollback cannot be confirmed because the saved plan names no app; no Wodby 2 resource was deleted")
	}
	if approved {
		fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "Rollback approved with --yes."))
		return nil
	}
	if planOutputJSON(cmd) {
		return errors.New("--output json requires --yes when rolling a migration back")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Type %q to confirm: ", appName)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.Wrap(err, "read rollback confirmation")
	}
	if strings.TrimSpace(line) != appName {
		return errors.New("rollback canceled; no Wodby 2 resource was deleted")
	}
	return nil
}

func reviewedPlanAppName(plan Plan) string {
	if len(plan.Apps) == 0 {
		return ""
	}
	return plan.Apps[0].Name
}

func confirmApply(cmd *RunEnv, approved bool) error {
	if approved {
		if !planOutputJSON(cmd) {
			fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "Migration approved with --yes."))
		}
		return nil
	}
	if planOutputJSON(cmd) {
		return errors.New("--output json requires --yes when starting a new migration apply")
	}
	fmt.Fprint(cmd.OutOrStdout(), "\nProceed with this migration? [y/N] ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errors.Wrap(err, "read migration confirmation")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "yes" {
		return errors.New("migration canceled; no plan, state, or target resource was created")
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "Migration approved."))
	return nil
}

// confirmRestart is the only approval requested for a restart. When cleanup
// deletes target resources, typing a deliberate phrase authorizes both that
// deletion and the fresh plan shown immediately above it.
func confirmRestart(cmd *RunEnv, approved bool, confirmation string, destructive bool) error {
	if approved {
		if !planOutputJSON(cmd) {
			fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "Restart and fresh migration approved with --yes."))
		}
		return nil
	}
	if planOutputJSON(cmd) {
		return errors.New("--output json requires --yes when restarting a migration")
	}
	if !destructive {
		fmt.Fprint(cmd.OutOrStdout(), "\nReplace the saved plan and state, then apply this fresh migration? [y/N] ")
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return errors.Wrap(err, "read restart confirmation")
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			return errors.New("restart canceled; the saved plan and state were kept and no Wodby 2 resource was deleted")
		}
	} else {
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"\nType %q to delete the resources listed above and apply this fresh migration: ",
			confirmation,
		)
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return errors.Wrap(err, "read restart confirmation")
		}
		if strings.TrimSpace(line) != confirmation {
			return errors.New("restart canceled; the saved plan and state were kept and no Wodby 2 resource was deleted")
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, "Restart and fresh migration approved."))
	return nil
}

func printSingleResumeNotice(
	cmd *RunEnv,
	planPath string,
	statePath string,
	state *MigrationState,
) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	totalSteps := 3
	if state != nil && state.App.TargetID > 0 {
		totalSteps = 4
	}
	fmt.Fprintln(w, "Resume setup")
	fmt.Fprintf(w, "\nStep 1/%d: Load saved migration\n", totalSteps)
	fmt.Fprintln(w, "  Saved plan: found")
	fmt.Fprintln(w, "  Resume state: found")
	if state != nil {
		fmt.Fprintf(w, "  Status: %s\n", migrationStatusLabel(state.Status))
		fmt.Fprintf(w, "  Resume from: %s\n", migrationPhaseLabel(state.Phase))
	}
	if cmd.Verbose {
		fmt.Fprintf(w, "  Plan file: %s\n", planPath)
		fmt.Fprintf(w, "  State file: %s\n", statePath)
	}

	fmt.Fprintf(w, "\nStep 2/%d: Select run mode\n", totalSteps)
	fmt.Fprintln(w, "  Mode: continue the saved plan")
	fmt.Fprintln(w, "  Reason: --restart was not provided")
	fmt.Fprintln(w, "  Completed operations will be reused")

	fmt.Fprintf(w, "\nStep 3/%d: Apply command options\n", totalSteps)
	fmt.Fprintln(w, "  Source backups: use the snapshots pinned by the saved plan")
	fmt.Fprintln(w, "  Protected download URLs will be refreshed without changing snapshots")
}

func printServerResumeNotice(cmd *RunEnv, planPath string, statePath string, states int) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "Server migration resume setup")
	fmt.Fprintln(w, "\nStep 1/3: Load saved migration")
	fmt.Fprintln(w, "  Saved server plan: found")
	fmt.Fprintf(w, "  Per-app resume states: %d found\n", states)
	if cmd.Verbose {
		fmt.Fprintf(w, "  Plan file: %s\n", planPath)
		fmt.Fprintf(w, "  State-file base: %s\n", statePath)
	}

	fmt.Fprintln(w, "\nStep 2/3: Select run mode")
	fmt.Fprintln(w, "  Mode: continue the saved server plan")
	fmt.Fprintln(w, "  Reason: --restart was not provided")
	fmt.Fprintln(w, "  Completed per-app operations will be reused")

	fmt.Fprintln(w, "\nStep 3/3: Apply command options")
	fmt.Fprintln(w, "  Source backups: use the snapshots pinned by the saved server plan")
	fmt.Fprintln(w, "  Protected download URLs will be refreshed without changing snapshots")
}

func printSingleResumeTargetValidation(cmd *RunEnv, targetAppID int) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "\nStep 4/4: Validate saved target")
	fmt.Fprintf(w, "  Target app ID: %d\n", targetAppID)
}

func printSingleResumeTargetValidated(cmd *RunEnv) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "  Target app: found")
}

func migrationStatusLabel(status MigrationStatus) string {
	switch status {
	case MigrationStatusInitialized:
		return "Ready"
	case MigrationStatusRunning:
		return "Running"
	case MigrationStatusFailed:
		return "Stopped"
	case MigrationStatusComplete:
		return "Complete"
	default:
		return string(status)
	}
}

func migrationPhaseLabel(phase MigrationPhase) string {
	switch phase {
	case MigrationPhasePlan:
		return "Planning"
	case MigrationPhasePrepare:
		return "Target preparation"
	case MigrationPhaseSyncData:
		return "Data import"
	case MigrationPhaseVerify:
		return "Verification"
	default:
		return string(phase)
	}
}

func printRestartConflict(
	cmd *RunEnv,
	sourceKind string,
	sourceID string,
	cleanups []migrationRestartCleanup,
	hasStateOnly bool,
	rollbackAvailable bool,
	planPath string,
	statePath string,
) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	destructive := false
	for _, cleanup := range cleanups {
		destructive = destructive || cleanup.Plan.DeletesResources()
	}
	headingColor := cliColorBold + cliColorOrange
	heading := "RESTART PAUSED: confirmation required"
	if destructive {
		headingColor = cliColorBold + cliColorRed
		heading = "RESTART BLOCKED: destructive cleanup approval required"
	}
	fmt.Fprintln(w, cliColor(w, headingColor, "\n"+heading))
	fmt.Fprintln(w, cliColor(w, cliColorBold, "Existing saved migration found"))
	fmt.Fprintf(w, "Source: %s %s\n", sourceKind, sourceID)
	fmt.Fprintln(w, "A fresh migration cannot start until the saved migration is either continued or cleaned up.")
	cleanupHeading := "\nCleanup required before restart"
	if destructive {
		fmt.Fprintln(w, cliColor(w, cliColorBold+cliColorRed, cleanupHeading))
	} else {
		fmt.Fprintln(w, cliColor(w, cliColorBold, cleanupHeading))
	}
	if len(cleanups) == 0 {
		fmt.Fprintln(w, "No migration-created Wodby 2 resources need to be deleted.")
	}
	for index, cleanup := range cleanups {
		if len(cleanups) > 1 {
			fmt.Fprintf(w, "\nApp %d/%d: %s\n", index+1, len(cleanups), cleanup.AppName)
		}
		fmt.Fprint(w, cleanup.Plan.DescribeRestart(cleanup.AppName))
	}
	if hasStateOnly {
		fmt.Fprintln(w, "Saved state without target mutations will be replaced.")
	}
	fmt.Fprintln(w, "\nChoose how to proceed:")
	fmt.Fprintln(w, "  Continue saved migration  Stop now and rerun the same --apply command without --restart")
	if rollbackAvailable {
		fmt.Fprintln(w, "  Clean up and stop          Stop now and rerun the same command with --rollback instead of --apply --restart")
	}
	fmt.Fprintln(w, "  Restart now               Confirm below to clean up and immediately apply the fresh plan")
	fmt.Fprintln(w, "\nNothing has been deleted and the fresh migration has not started.")
	if cmd.Verbose {
		fmt.Fprintf(w, "  Plan file: %s\n", planPath)
		fmt.Fprintf(w, "  Resume-state file: %s\n", statePath)
	}
}

// showUnpreparedPlan prints a blocked plan whose target was not inspected.
func showUnpreparedPlan(cmd *RunEnv, plan Plan, apply, verify bool) error {
	if !apply && !verify {
		return printPreview(cmd, plan)
	}
	action := "apply"
	if verify {
		action = "verify"
	}
	return rejectBlockedMigrationAction(cmd, plan, action)
}

func rejectBlockedMigrationAction(cmd *RunEnv, plan Plan, action string, prepared ...PreparedMigration) error {
	if !planOutputJSON(cmd) {
		PrintReview(cmd.OutOrStdout(), plan, prepared...)
		printInstanceExclusionHints(cmd, plan)
	}
	return errors.Errorf("cannot %s migration: resolve the %d blocking review item(s) shown above", action, plan.Summary.Blocking)
}

func printInstanceExclusionHints(cmd *RunEnv, plan Plan) {
	if plan.Source.Kind != "app" && plan.Source.Kind != "server" {
		return
	}
	blocked := map[string]bool{}
	for _, item := range plan.Review {
		if item.Severity != SeverityBlocking || item.Instance == "" {
			continue
		}
		for _, app := range plan.Apps {
			if item.App != "" && item.App != app.Name && item.App != app.Title {
				continue
			}
			for _, instance := range app.Instances {
				if item.Instance == instance.Name || item.Instance == instance.Title || item.Instance == instance.SourceUUID {
					blocked[instance.SourceUUID] = true
				}
			}
		}
	}
	if len(blocked) == 0 {
		return
	}
	ids := make([]string, 0, len(blocked))
	for id := range blocked {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Fprintln(cmd.OutOrStdout(), "To intentionally omit a blocked instance, rerun the preview with:")
	for _, id := range ids {
		fmt.Fprintf(cmd.OutOrStdout(), "  --exclude-instance %s\n", id)
	}
}

func printArtifactNotice(cmd *RunEnv, planPath, statePath string) {
	if planOutputJSON(cmd) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary migration plan: %s\n", planPath)
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary resume state: %s\n", statePath)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary resume-state file: %s\n", statePath)
	fmt.Fprintln(cmd.OutOrStdout(), "Keep these files until --verify succeeds. Starting migration...")
}

func printServerArtifactNotice(cmd *RunEnv, planPath, statePath string) {
	if planOutputJSON(cmd) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary migration plan: %s\n", planPath)
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary per-app resume-state base: %s\n", statePath)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary per-app resume-state base: %s\n", statePath)
	fmt.Fprintln(cmd.OutOrStdout(), "The result will list each exact state path. Keep them until --verify succeeds. Starting migration...")
}

func printMigrationResult(
	cmd *RunEnv,
	action string,
	plan Plan,
	planPath string,
	statePath string,
) error {
	output := migrationOutput{
		Action:    action,
		Plan:      plan,
		PlanFile:  planPath,
		StateFile: statePath,
		Status:    publicMigrationStatus(action == "apply"),
	}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(output))
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), cliColorGreen, fmt.Sprintf("Migration %s completed.", action)))
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", publicMigrationStatus(action == "apply"))
	printImportStatuses(cmd, plan, "completed")
	if action == "apply" {
		fmt.Fprintln(cmd.OutOrStdout(), "Test the app using its Wodby 2 technical route before changing DNS.")
		fmt.Fprintln(cmd.OutOrStdout(), "After DNS points to Wodby 2, rerun the same command with --verify.")
		fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
		fmt.Fprintf(cmd.OutOrStdout(), "Temporary resume-state file: %s\n", statePath)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Target resources, DNS, routes, certificates, and data imports match the applied migration.")
	}
	return nil
}

// pausedActionOutput is the machine-readable form of one pending external
// action, so automation can pick the app service to build without scraping
// the human-facing text.
type pausedActionOutput struct {
	Instance         string `json:"instance"`
	TargetInstanceID int    `json:"targetAppInstanceId"`
	Service          string `json:"service"`
	TargetServiceID  int    `json:"targetAppServiceId"`
	CIProvider       string `json:"ciProvider,omitempty"`
	ExampleURL       string `json:"exampleUrl,omitempty"`
	GitRef           string `json:"gitRef,omitempty"`
	// WaitsFor is custom_ci_build, or manual_code_deploy for a --skip-code
	// migration whose data import needs code the operator deploys.
	WaitsFor        string `json:"waitsFor"`
	ImportComponent string `json:"importComponent,omitempty"`
}

type migrationPausedOutput struct {
	Action    string               `json:"action"`
	PlanFile  string               `json:"planFile"`
	StateFile string               `json:"stateFile"`
	Status    string               `json:"status"`
	Reason    string               `json:"reason"`
	Actions   []pausedActionOutput `json:"pendingExternalActions"`
}

// printMigrationPaused reports a migration that stopped on a required external
// action. This is not a failure: the target is intact and the same --apply
// command resumes it, so the output explains what to run instead of asking the
// operator to inspect a healthy target. The error is still returned so the exit
// status stays non-zero for scripted callers.
func printMigrationPaused(
	cmd *RunEnv,
	paused *MigrationPausedError,
	planPath string,
	statePath string,
) error {
	// The run already printed everything an operator needs; the caller should not
	// append usage or a duplicate error line on top of it.
	cmd.silenceErrors()

	if planOutputJSON(cmd) {
		output := migrationPausedOutput{
			Action:    "apply",
			PlanFile:  planPath,
			StateFile: statePath,
			Status:    pausedStatus(paused),
			Reason:    paused.Error(),
			Actions:   make([]pausedActionOutput, 0, len(paused.Actions)),
		}
		for _, action := range paused.Actions {
			output.Actions = append(output.Actions, pausedActionOutput{
				Instance:         action.Instance,
				TargetInstanceID: action.TargetInstanceID,
				Service:          action.ServiceName,
				TargetServiceID:  action.TargetServiceID,
				CIProvider:       action.ProviderLabel,
				ExampleURL:       action.ExampleURL,
				GitRef:           action.GitRef,
				WaitsFor:         pausedActionWaitsFor(action),
				ImportComponent:  action.ImportComponent,
			})
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(output); err != nil {
			return errors.WithStack(err)
		}
		return paused
	}

	w := cmd.OutOrStdout()
	title := "\nMigration paused: action required outside Wodby"
	if paused.ManualCodeDeployOnly() {
		title = "\nMigration paused: deploy your code to continue"
	}
	fmt.Fprintln(w, cliColor(w, cliColorBold+cliColorOrange, title))
	fmt.Fprintln(w)
	fmt.Fprint(w, paused.NextSteps())
	fmt.Fprintf(w, "\nTemporary plan file: %s\n", planPath)
	fmt.Fprintf(w, "Temporary resume-state file: %s\n", statePath)
	fmt.Fprintln(w, "Keep both files; they are how the rerun resumes instead of starting over.")
	return paused
}

const (
	pausedMigrationStatus  = "paused; awaiting external CI build"
	pausedCodeDeployStatus = "paused; awaiting manual code deployment"
)

// pausedStatus keeps the Custom CI status for any pause that includes a CI
// build, so scripts written for it keep working.
func pausedStatus(paused *MigrationPausedError) string {
	if paused.ManualCodeDeployOnly() {
		return pausedCodeDeployStatus
	}
	return pausedMigrationStatus
}

func pausedActionWaitsFor(action *ExternalActionRequiredError) string {
	if action.ManualCodeDeploy {
		return "manual_code_deploy"
	}
	return "custom_ci_build"
}

func publicMigrationStatus(apply bool) string {
	if apply {
		return "applied; awaiting DNS verification"
	}
	return "verified"
}

func printServerMigrationResult(
	cmd *RunEnv,
	action string,
	plan Plan,
	planPath string,
	apps []serverAppMigrationOutput,
) error {
	output := serverMigrationOutput{Action: action, Plan: plan, PlanFile: planPath, Apps: apps}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(output))
	}
	// A paused app waits for a third-party CI build or a manual code
	// deployment and carries the next step in its error text; it has not failed.
	failed, paused := 0, 0
	for _, app := range apps {
		switch {
		case app.Status == pausedMigrationStatus || app.Status == pausedCodeDeployStatus:
			paused++
		case app.Error != "":
			failed++
		}
	}
	color := cliColorGreen
	message := fmt.Sprintf("Server migration %s completed for %d app(s).", action, len(apps))
	if failed != 0 || paused != 0 {
		color = cliColorOrange
		message = fmt.Sprintf("Server migration %s processed %d app(s): %d succeeded, %d paused, %d failed.", action, len(apps), len(apps)-failed-paused, paused, failed)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), color, message))
	for _, app := range apps {
		fmt.Fprintf(cmd.OutOrStdout(), "App %s: status=%s\n", app.Name, app.Status)
		if cmd.Verbose {
			fmt.Fprintf(cmd.OutOrStdout(), "  Resume-state file: %s\n", app.StateFile)
		}
		if app.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  Error: %s\n", app.Error)
		}
	}
	// The import table describes the whole plan, so it is shown as completed
	// only when every app finished.
	if failed == 0 && paused == 0 {
		printImportStatuses(cmd, plan, "completed")
	}
	if action == "apply" {
		fmt.Fprintln(cmd.OutOrStdout(), "Test every app using its Wodby 2 technical route before changing DNS.")
		fmt.Fprintln(cmd.OutOrStdout(), "After DNS points to Wodby 2, rerun the same command with --verify.")
		if cmd.Verbose {
			fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
		}
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Target resources, DNS, routes, certificates, and data imports match the applied migrations.")
	}
	return nil
}

func printImportStatuses(cmd *RunEnv, plan Plan, status string) {
	rows := 0
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, item := range instance.Imports {
				if item.Action == "import" {
					rows++
				}
			}
		}
	}
	if rows == 0 {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nData imports:")
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  App\tInstance\tComponent\tTarget\tStatus")
	fmt.Fprintln(tw, "  ---\t--------\t---------\t------\t------")
	for _, app := range plan.Apps {
		for _, instance := range app.Instances {
			for _, item := range instance.Imports {
				if item.Action != "import" {
					continue
				}
				fmt.Fprintf(
					tw,
					"  %s\t%s\t%s\t%s:%s\t%s\n",
					app.Name,
					instance.Name,
					item.Component,
					item.TargetService,
					item.TargetImport,
					status,
				)
			}
		}
	}
	_ = tw.Flush()
}

func planOutputJSON(cmd *RunEnv) bool {
	return cmd.JSON
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
