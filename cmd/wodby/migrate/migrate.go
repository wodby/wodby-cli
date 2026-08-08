package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
	"github.com/wodby/wodby-cli/pkg/types"
)

const (
	defaultSourceBaseURL = "https://api.wodby.com"
	sourceTokenEnv       = "WODBY1_SOURCE_TOKEN"
)

type options struct {
	sourceBaseURL string
	sourceToken   string
	phase         string

	targetOrg        string
	targetProject    string
	targetCluster    string
	targetEnvMap     []string
	targetStackMap   []string
	targetServiceMap []string
	targetImportMap  []string

	targetCIIntegrationID int
	targetRemoteGitRepoID string
	targetRepositoryMap   []string
	targetCodeService     string
	targetGitRef          string
	targetGitRefType      string

	skipCode bool
	skipData bool

	approvePlan    string
	planFile       string
	stateFile      string
	pollInterval   time.Duration
	waitTimeout    time.Duration
	maxBackupAge   time.Duration
	retryAmbiguous string
	output         string
}

type phaseOutput struct {
	Phase     wodby1.MigrationPhase  `json:"phase"`
	PlanHash  string                 `json:"planHash"`
	StateFile string                 `json:"stateFile"`
	Status    wodby1.MigrationStatus `json:"status"`
}

type serverAppPhaseOutput struct {
	SourceAppUUID string                 `json:"sourceAppUuid"`
	Name          string                 `json:"name"`
	StateFile     string                 `json:"stateFile"`
	Status        wodby1.MigrationStatus `json:"status"`
}

type serverPhaseOutput struct {
	Phase    wodby1.MigrationPhase  `json:"phase"`
	PlanHash string                 `json:"planHash"`
	Apps     []serverAppPhaseOutput `json:"apps"`
}

type serverAppExecution struct {
	sourceAppUUID string
	name          string
	statePath     string
	export        wodby1.Export
	plan          wodby1.Plan
	prepared      wodby1.PreparedMigration
	executor      *wodby1.MigrationExecutor
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate applications to Wodby 2",
	}
	cmd.AddCommand(newWodby1Command())
	return cmd
}

func newWodby1Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wodby1",
		Short: "Migrate Wodby 1 applications to Wodby 2",
	}
	cmd.AddCommand(newWodby1AppCommand(), newWodby1ServerCommand())
	return cmd
}

func newWodby1ServerCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "server SOURCE_SERVER_UUID",
		Short: "Plan or run every application migration from a Wodby 1 server",
		Long: `Migrate every application hosted on one Wodby 1 server through one
reviewed plan. Each application has an isolated resume-state file and is
processed sequentially. If multiple source applications have repositories,
use --target-repository-map once per app or intentionally use --skip-code.
Before sync-data, enable maintenance mode and create fresh backups for every
source instance. Change DNS only after validating the prepared target and
syncing data; finalize then validates DNS and deploys the staged routes.`,
		Example: `  export WODBY1_SOURCE_TOKEN=...
  export WODBY_API_KEY=...

  wodby migrate wodby1 server SERVER_UUID --phase plan --skip-code [target and mapping options]
  wodby migrate wodby1 server SERVER_UUID --phase prepare --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 server SERVER_UUID --phase sync-data --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 server SERVER_UUID --phase finalize --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 server SERVER_UUID --phase verify --approve-plan PLAN_HASH [same options]`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1Server(cmd, args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	cmd.Flags().StringArrayVar(
		&opts.targetRepositoryMap,
		"target-repository-map",
		nil,
		"Per-app Git target (APP=CI_INTEGRATION_ID:REMOTE_GIT_REPO_ID[:SERVICE])",
	)
	cmd.Flags().Lookup("state-file").Usage = "Base path for secure per-app resume-state files (defaults beside the plan)"
	return cmd
}

func newWodby1AppCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "app SOURCE_APP_UUID",
		Short: "Plan or run a resumable Wodby 1 application migration",
		Long: `Migrate one Wodby 1 application through plan, prepare, sync-data,
finalize, and verify. Reuse the same Wodby 1 token, Wodby 2 API key, mapping
options, plan path, and state path for every phase. Before sync-data, enable
maintenance mode and create a fresh backup in Wodby 1. Update DNS only after
sync-data succeeds and before finalize. If a target mutation is ambiguous,
inspect Wodby 2 and pass --retry-ambiguous only with the exact operation ID
printed by the command.`,
		Example: `  export WODBY1_SOURCE_TOKEN=...
  export WODBY_API_KEY=...

  wodby migrate wodby1 app APP_UUID --phase plan [target and mapping options]
  wodby migrate wodby1 app APP_UUID --phase prepare --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 app APP_UUID --phase sync-data --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 app APP_UUID --phase finalize --approve-plan PLAN_HASH [same options]
  wodby migrate wodby1 app APP_UUID --phase verify --approve-plan PLAN_HASH [same options]`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1App(cmd, args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	return cmd
}

func defaultOptions() *options {
	return &options{
		sourceBaseURL: defaultSourceBaseURL,
		phase:         "plan",
		pollInterval:  2 * time.Second,
		waitTimeout:   30 * time.Minute,
		maxBackupAge:  time.Hour,
		output:        "text",
	}
}

func bindFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.sourceBaseURL, "source-base-url", defaultSourceBaseURL, "Wodby 1 API base URL")
	cmd.Flags().StringVar(&opts.sourceToken, "source-token", "", "Wodby 1 API token (defaults to "+sourceTokenEnv+")")
	cmd.Flags().StringVar(&opts.phase, "phase", "plan", "Migration phase: plan, prepare, sync-data, finalize, or verify")

	cmd.Flags().StringVar(&opts.targetOrg, "target-org", "", "Wodby 2 organization ID or exact name")
	cmd.Flags().StringVar(&opts.targetProject, "target-project", "", "Wodby 2 project ID or exact name")
	cmd.Flags().StringVar(&opts.targetCluster, "target-cluster", "", "Wodby 2 cluster ID or exact name")
	cmd.Flags().StringArrayVar(&opts.targetEnvMap, "target-env-map", nil, "Source-to-target environment mapping (SOURCE=TARGET)")
	cmd.Flags().StringArrayVar(&opts.targetStackMap, "target-stack-map", nil, "Source-to-target stack mapping ([APP/][INSTANCE/]SOURCE=TARGET)")
	cmd.Flags().StringArrayVar(&opts.targetServiceMap, "target-service-map", nil, "Source-to-target service mapping ([APP/][INSTANCE/]SOURCE=TARGET)")
	cmd.Flags().StringArrayVar(&opts.targetImportMap, "target-import-map", nil, "Backup-to-import mapping ([APP/][INSTANCE/]COMPONENT=SERVICE:IMPORT)")

	cmd.Flags().IntVar(&opts.targetCIIntegrationID, "target-ci-integration-id", 0, "Wodby 2 Git integration ID")
	cmd.Flags().StringVar(&opts.targetRemoteGitRepoID, "target-remote-git-repo-id", "", "Repository ID in the selected Wodby 2 Git integration")
	cmd.Flags().StringVar(&opts.targetCodeService, "target-code-service", "", "Target connect-build service name when the stack has more than one")
	cmd.Flags().StringVar(&opts.targetGitRef, "target-git-ref", "", "Git branch, tag, or commit to build (defaults to the source ref)")
	cmd.Flags().StringVar(&opts.targetGitRefType, "target-git-ref-type", "", "Git ref type: branch, tag, or commit")

	cmd.Flags().BoolVar(&opts.skipCode, "skip-code", false, "Intentionally omit repository/build-source migration")
	cmd.Flags().BoolVar(&opts.skipData, "skip-data", false, "Intentionally omit database and files imports")

	cmd.Flags().StringVar(&opts.approvePlan, "approve-plan", "", "Exact reviewed plan hash required by mutation phases")
	cmd.Flags().StringVar(&opts.planFile, "plan-file", "", "Plan JSON path (defaults beside the current working directory)")
	cmd.Flags().StringVar(&opts.stateFile, "state-file", "", "Secure resume-state path (defaults beside the plan)")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", 2*time.Second, "Target operation polling interval")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Timeout for each target operation")
	cmd.Flags().DurationVar(&opts.maxBackupAge, "max-backup-age", time.Hour, "Maximum backup age accepted by sync-data")
	cmd.Flags().StringVar(&opts.retryAmbiguous, "retry-ambiguous", "", "Retry exactly one inspected ambiguous operation ID")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "Output format: text or json")
}

func runWodby1App(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	opts.sourceToken = firstNonBlank(opts.sourceToken, os.Getenv(sourceTokenEnv))
	phase, err := parsePhase(opts.phase)
	if err != nil {
		return err
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	opts.sourceToken, err = wodby1.NormalizeSourceToken(opts.sourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseMapping(opts.targetStackMap, "--target-stack-map")
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.targetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	importMap, err := parseMapping(opts.targetImportMap, "--target-import-map")
	if err != nil {
		return err
	}
	sourceClient, err := wodby1.NewSourceClient(opts.sourceBaseURL, opts.sourceToken)
	if err != nil {
		return err
	}

	planPath, statePath, err := artifactPaths(sourceID, opts.planFile, opts.stateFile)
	if err != nil {
		return err
	}
	stateLock, err := wodby1.AcquireMigrationStateLock(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := stateLock.Close(); runErr == nil && err != nil {
			runErr = err
		}
	}()

	var reviewedPlan *wodby1.Plan
	allowedTargetAppID := 0
	allowTargetAppRecovery := false
	if phase != wodby1.MigrationPhasePlan {
		reviewed, err := wodby1.LoadReviewedPlan(planPath)
		if err != nil {
			return errors.Wrap(err, "load reviewed migration plan")
		}
		if strings.TrimSpace(opts.approvePlan) != reviewed.PlanHash {
			return errors.Errorf(
				"mutation phase %s requires --approve-plan %s after reviewing %s",
				phaseLabel(phase),
				reviewed.PlanHash,
				planPath,
			)
		}
		if reviewed.Status == "blocked" || reviewed.Summary.Blocking != 0 {
			return errors.Errorf(
				"reviewed migration plan %s is blocked by %d review item(s); inspect %s",
				reviewed.PlanHash,
				reviewed.Summary.Blocking,
				planPath,
			)
		}
		allowedTargetAppID, allowTargetAppRecovery, err = stateBackedTargetApp(statePath, reviewed)
		if err != nil {
			return err
		}
		reviewedPlan = &reviewed
	}

	targetClient, err := wodby1.NewTargetClient(types.APIConfig{
		Endpoint: strings.TrimSpace(viper.GetString("api_base_url")),
		Key:      strings.TrimSpace(viper.GetString("api_key")),
	})
	if err != nil {
		return err
	}

	// Verify the caller's role in the selected destination organization before
	// asking Wodby 1 for the secret-bearing export.
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), wodby1.TargetScopeSelectors{
		Org:     opts.targetOrg,
		Project: opts.targetProject,
		Cluster: opts.targetCluster,
	})
	if err != nil {
		return err
	}

	// The customer command transfers required credentials in memory. Wodby 1
	// independently authorizes this export using the source organization role.
	export, err := sourceClient.ExportApp(cmd.Context(), sourceID)
	if err != nil {
		return err
	}

	selectors, err := wodby1.TargetEnvironmentSelectors(export, envMap)
	if err != nil {
		return err
	}
	resolved, err := targetClient.ResolveTargetEnvs(cmd.Context(), scope.Org.ID, selectors)
	if err != nil {
		return err
	}
	targetEnvs := make(map[string]wodby1.TargetEnv, len(resolved))
	for _, item := range resolved {
		targetEnvs[item.Selector] = item.Env
	}

	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:                    "app",
		SourceID:                      sourceID,
		TargetOrg:                     opts.targetOrg,
		TargetProject:                 opts.targetProject,
		TargetCluster:                 opts.targetCluster,
		TargetEnvMap:                  envMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackMap:                stackMap,
		TargetServiceMap:              serviceMap,
		TargetImportMap:               importMap,
		Repository: wodby1.RepositoryTargetPlan{
			CIIntegrationID: opts.targetCIIntegrationID,
			RemoteGitRepoID: strings.TrimSpace(opts.targetRemoteGitRepoID),
			Service:         strings.TrimSpace(opts.targetCodeService),
		},
		SkipCode:    opts.skipCode,
		SkipData:    opts.skipData,
		RequireData: !opts.skipData,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := wodby1.PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, wodby1.TargetPreflightOptions{
		SkipCode:                    opts.skipCode,
		SkipData:                    opts.skipData,
		GitRef:                      opts.targetGitRef,
		GitRefType:                  opts.targetGitRefType,
		AllowedTargetAppID:          allowedTargetAppID,
		AllowStateBackedAppRecovery: allowTargetAppRecovery,
	})
	if err != nil {
		return err
	}

	if phase == wodby1.MigrationPhasePlan {
		if err := writePlanFile(planPath, plan); err != nil {
			return err
		}
		return printPlan(cmd, plan, planPath)
	}
	if plan.PlanHash != reviewedPlan.PlanHash {
		return errors.Errorf(
			"current source or migration options no longer match reviewed plan %s; run the plan phase again and review the new plan",
			reviewedPlan.PlanHash,
		)
	}
	if plan.Summary.Blocking != 0 {
		return errors.Errorf(
			"migration plan %s is blocked by %d review item(s); inspect %s",
			plan.PlanHash,
			plan.Summary.Blocking,
			planPath,
		)
	}

	executor, err := wodby1.NewMigrationExecutor(targetClient, wodby1.MigrationExecutorOptions{
		StatePath:               statePath,
		PollInterval:            opts.pollInterval,
		OperationTimeout:        opts.waitTimeout,
		MaxBackupAge:            opts.maxBackupAge,
		RetryAmbiguousOperation: opts.retryAmbiguous,
		RefreshSource: func(ctx context.Context) (wodby1.Export, error) {
			return sourceClient.ExportApp(ctx, sourceID)
		},
	})
	if err != nil {
		return err
	}

	var result wodby1.MigrationPhaseResult
	switch phase {
	case wodby1.MigrationPhasePrepare:
		result, err = executor.Prepare(cmd.Context(), export, plan, prepared)
	case wodby1.MigrationPhaseSyncData:
		result, err = executor.SyncData(cmd.Context(), export, plan, prepared)
	case wodby1.MigrationPhaseFinalize:
		result, err = executor.Finalize(cmd.Context(), export, plan, prepared, scope.Cluster)
	case wodby1.MigrationPhaseVerify:
		result, err = executor.Verify(cmd.Context(), export, plan, prepared)
	default:
		return errors.Errorf("unsupported migration phase %q", phase)
	}
	if err != nil {
		return err
	}
	return printPhaseResult(cmd, plan, planPath, statePath, result)
}

func runWodby1Server(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	opts.sourceToken = firstNonBlank(opts.sourceToken, os.Getenv(sourceTokenEnv))
	phase, err := parsePhase(opts.phase)
	if err != nil {
		return err
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	opts.sourceToken, err = wodby1.NormalizeSourceToken(opts.sourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseMapping(opts.targetStackMap, "--target-stack-map")
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.targetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	importMap, err := parseMapping(opts.targetImportMap, "--target-import-map")
	if err != nil {
		return err
	}
	repositoryMap, err := parseRepositoryMapping(opts.targetRepositoryMap)
	if err != nil {
		return err
	}
	sourceClient, err := wodby1.NewSourceClient(opts.sourceBaseURL, opts.sourceToken)
	if err != nil {
		return err
	}

	planPath, statePath, err := artifactPaths(sourceID, opts.planFile, opts.stateFile)
	if err != nil {
		return err
	}
	stateLock, err := wodby1.AcquireMigrationStateLock(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := stateLock.Close(); runErr == nil && err != nil {
			runErr = err
		}
	}()

	var reviewedPlan *wodby1.Plan
	if phase != wodby1.MigrationPhasePlan {
		reviewed, err := wodby1.LoadReviewedPlan(planPath)
		if err != nil {
			return errors.Wrap(err, "load reviewed migration plan")
		}
		if reviewed.Source.Kind != "server" || reviewed.Source.ID != sourceID {
			return errors.New("reviewed migration plan does not match the requested source server")
		}
		if strings.TrimSpace(opts.approvePlan) != reviewed.PlanHash {
			return errors.Errorf(
				"mutation phase %s requires --approve-plan %s after reviewing %s",
				phaseLabel(phase),
				reviewed.PlanHash,
				planPath,
			)
		}
		if reviewed.Status == "blocked" || reviewed.Summary.Blocking != 0 {
			return errors.Errorf(
				"reviewed migration plan %s is blocked by %d review item(s); inspect %s",
				reviewed.PlanHash,
				reviewed.Summary.Blocking,
				planPath,
			)
		}
		reviewedPlan = &reviewed
	}

	targetClient, err := wodby1.NewTargetClient(types.APIConfig{
		Endpoint: strings.TrimSpace(viper.GetString("api_base_url")),
		Key:      strings.TrimSpace(viper.GetString("api_key")),
	})
	if err != nil {
		return err
	}
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), wodby1.TargetScopeSelectors{
		Org:     opts.targetOrg,
		Project: opts.targetProject,
		Cluster: opts.targetCluster,
	})
	if err != nil {
		return err
	}

	export, err := sourceClient.ExportServer(cmd.Context(), sourceID)
	if err != nil {
		return err
	}
	if err := validateServerRepositoryOptions(export, opts); err != nil {
		return err
	}
	if err := validateServerArtifactPaths(planPath, statePath, export); err != nil {
		return err
	}

	selectors, err := wodby1.TargetEnvironmentSelectors(export, envMap)
	if err != nil {
		return err
	}
	resolved, err := targetClient.ResolveTargetEnvs(cmd.Context(), scope.Org.ID, selectors)
	if err != nil {
		return err
	}
	targetEnvs := make(map[string]wodby1.TargetEnv, len(resolved))
	for _, item := range resolved {
		targetEnvs[item.Selector] = item.Env
	}

	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:                    "server",
		SourceID:                      sourceID,
		TargetOrg:                     opts.targetOrg,
		TargetProject:                 opts.targetProject,
		TargetCluster:                 opts.targetCluster,
		TargetEnvMap:                  envMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackMap:                stackMap,
		TargetServiceMap:              serviceMap,
		TargetImportMap:               importMap,
		Repository: wodby1.RepositoryTargetPlan{
			CIIntegrationID: opts.targetCIIntegrationID,
			RemoteGitRepoID: strings.TrimSpace(opts.targetRemoteGitRepoID),
			Service:         strings.TrimSpace(opts.targetCodeService),
		},
		RepositoryByApp: repositoryMap,
		SkipCode:        opts.skipCode,
		SkipData:        opts.skipData,
		RequireData:     !opts.skipData,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := wodby1.PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}

	allowedTargetAppIDs := map[string]int{}
	stateBackedRecovery := map[string]bool{}
	if reviewedPlan != nil {
		for _, app := range reviewedPlan.Apps {
			_, childPlan, _, err := wodby1.ScopeServerMigrationApp(
				export,
				*reviewedPlan,
				wodby1.PreparedMigration{},
				app.SourceUUID,
				opts.sourceToken,
			)
			if err != nil {
				return err
			}
			childStatePath := serverAppStatePath(statePath, app.SourceUUID)
			targetID, allowRecovery, err := stateBackedTargetApp(childStatePath, childPlan)
			if err != nil {
				return err
			}
			allowedTargetAppIDs[app.SourceUUID] = targetID
			stateBackedRecovery[app.SourceUUID] = allowRecovery
		}
	}
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, wodby1.TargetPreflightOptions{
		SkipCode:               opts.skipCode,
		SkipData:               opts.skipData,
		GitRef:                 opts.targetGitRef,
		GitRefType:             opts.targetGitRefType,
		AllowedTargetAppIDs:    allowedTargetAppIDs,
		StateBackedAppRecovery: stateBackedRecovery,
	})
	if err != nil {
		return err
	}

	if phase == wodby1.MigrationPhasePlan {
		if err := writePlanFile(planPath, plan); err != nil {
			return err
		}
		return printPlan(cmd, plan, planPath)
	}
	if plan.PlanHash != reviewedPlan.PlanHash {
		return errors.Errorf(
			"current source or migration options no longer match reviewed plan %s; run the plan phase again and review the new plan",
			reviewedPlan.PlanHash,
		)
	}
	if plan.Summary.Blocking != 0 {
		return errors.Errorf(
			"migration plan %s is blocked by %d review item(s); inspect %s",
			plan.PlanHash,
			plan.Summary.Blocking,
			planPath,
		)
	}

	executions := make([]serverAppExecution, 0, len(prepared.Apps))
	for _, preparedApp := range prepared.Apps {
		appUUID := preparedApp.App.App.UUID
		childExport, childPlan, childPrepared, err := wodby1.ScopeServerMigrationApp(
			export,
			plan,
			prepared,
			appUUID,
			opts.sourceToken,
		)
		if err != nil {
			return err
		}
		childStatePath := serverAppStatePath(statePath, appUUID)
		executor, err := wodby1.NewMigrationExecutor(targetClient, wodby1.MigrationExecutorOptions{
			StatePath:               childStatePath,
			PollInterval:            opts.pollInterval,
			OperationTimeout:        opts.waitTimeout,
			MaxBackupAge:            opts.maxBackupAge,
			RetryAmbiguousOperation: opts.retryAmbiguous,
			RefreshSource: func(ctx context.Context) (wodby1.Export, error) {
				refreshed, err := sourceClient.ExportServer(ctx, sourceID)
				if err != nil {
					return wodby1.Export{}, err
				}
				return wodby1.ScopeServerExportApp(refreshed, appUUID, opts.sourceToken)
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
	if phase == wodby1.MigrationPhaseFinalize {
		for _, execution := range executions {
			if err := execution.executor.ValidateFinalize(
				cmd.Context(),
				execution.export,
				execution.plan,
				execution.prepared,
				scope.Cluster,
			); err != nil {
				return errors.Wrapf(
					err,
					"validate finalization for source app %s (%s) with state file %s",
					execution.name,
					execution.sourceAppUUID,
					execution.statePath,
				)
			}
		}
	}

	results := make([]serverAppPhaseOutput, 0, len(executions))
	for _, execution := range executions {
		var result wodby1.MigrationPhaseResult
		switch phase {
		case wodby1.MigrationPhasePrepare:
			result, err = execution.executor.Prepare(cmd.Context(), execution.export, execution.plan, execution.prepared)
		case wodby1.MigrationPhaseSyncData:
			result, err = execution.executor.SyncData(cmd.Context(), execution.export, execution.plan, execution.prepared)
		case wodby1.MigrationPhaseFinalize:
			result, err = execution.executor.Finalize(cmd.Context(), execution.export, execution.plan, execution.prepared, scope.Cluster)
		case wodby1.MigrationPhaseVerify:
			result, err = execution.executor.Verify(cmd.Context(), execution.export, execution.plan, execution.prepared)
		default:
			return errors.Errorf("unsupported migration phase %q", phase)
		}
		if err != nil {
			return errors.Wrapf(
				err,
				"migrate source app %s (%s) with state file %s",
				execution.name,
				execution.sourceAppUUID,
				execution.statePath,
			)
		}
		results = append(results, serverAppPhaseOutput{
			SourceAppUUID: execution.sourceAppUUID,
			Name:          execution.name,
			StateFile:     execution.statePath,
			Status:        result.State.Status,
		})
	}
	return printServerPhaseResult(cmd, plan, planPath, phase, results)
}

func validateOptions(opts *options) error {
	if strings.TrimSpace(opts.sourceBaseURL) == "" {
		return errors.New("--source-base-url is required")
	}
	if _, err := url.ParseRequestURI(opts.sourceBaseURL); err != nil {
		return errors.Wrap(err, "invalid --source-base-url")
	}
	if strings.TrimSpace(opts.sourceToken) == "" {
		return errors.Errorf("--source-token or %s is required", sourceTokenEnv)
	}
	if opts.output != "text" && opts.output != "json" {
		return errors.Errorf("unsupported --output %q", opts.output)
	}
	if !hasTarget(opts) {
		return errors.New("--target-org, --target-project, and --target-cluster are required")
	}
	if strings.TrimSpace(viper.GetString("api_base_url")) == "" {
		return errors.New("--api-base-url is required")
	}
	if strings.TrimSpace(viper.GetString("api_key")) == "" {
		return errors.New("--api-key is required; Wodby 2 access tokens cannot authorize customer migrations")
	}
	if opts.pollInterval <= 0 {
		return errors.New("--poll-interval must be positive")
	}
	if opts.waitTimeout <= 0 {
		return errors.New("--wait-timeout must be positive")
	}
	if opts.maxBackupAge <= 0 {
		return errors.New("--max-backup-age must be positive")
	}
	if opts.targetCIIntegrationID < 0 {
		return errors.New("--target-ci-integration-id cannot be negative")
	}
	refType := strings.ToLower(strings.TrimSpace(opts.targetGitRefType))
	if refType != "" && refType != "branch" && refType != "tag" && refType != "commit" {
		return errors.New("--target-git-ref-type must be branch, tag, or commit")
	}
	if (strings.TrimSpace(opts.targetGitRef) == "") != (refType == "") {
		return errors.New("--target-git-ref and --target-git-ref-type must be specified together")
	}
	return nil
}

func parsePhase(value string) (wodby1.MigrationPhase, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "plan":
		return wodby1.MigrationPhasePlan, nil
	case "prepare":
		return wodby1.MigrationPhasePrepare, nil
	case "sync-data", "sync_data":
		return wodby1.MigrationPhaseSyncData, nil
	case "finalize":
		return wodby1.MigrationPhaseFinalize, nil
	case "verify":
		return wodby1.MigrationPhaseVerify, nil
	default:
		return "", errors.Errorf("unsupported --phase %q", value)
	}
}

func phaseLabel(phase wodby1.MigrationPhase) string {
	return strings.ReplaceAll(string(phase), "_", "-")
}

func hasTarget(opts *options) bool {
	return strings.TrimSpace(opts.targetOrg) != "" &&
		strings.TrimSpace(opts.targetProject) != "" &&
		strings.TrimSpace(opts.targetCluster) != ""
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

func parseRepositoryMapping(values []string) (map[string]wodby1.RepositoryTargetPlan, error) {
	result := map[string]wodby1.RepositoryTargetPlan{}
	for _, value := range values {
		key, target, ok := strings.Cut(strings.TrimSpace(value), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || key == "" || strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("--target-repository-map value %q must use APP=CI_INTEGRATION_ID:REMOTE_GIT_REPO_ID[:SERVICE]", value)
		}
		parts := strings.SplitN(target, ":", 3)
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("--target-repository-map value %q must use APP=CI_INTEGRATION_ID:REMOTE_GIT_REPO_ID[:SERVICE]", value)
		}
		integrationID, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || integrationID <= 0 {
			return nil, fmt.Errorf("--target-repository-map value %q must contain a positive CI integration ID", value)
		}
		mapping := wodby1.RepositoryTargetPlan{
			CIIntegrationID: integrationID,
			RemoteGitRepoID: strings.TrimSpace(parts[1]),
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

func validateServerRepositoryOptions(export wodby1.Export, opts *options) error {
	if opts == nil || opts.skipCode {
		return nil
	}
	repositories := 0
	for _, app := range export.AppExports() {
		if app.App.Repository != nil {
			repositories++
		}
	}
	if repositories > 1 &&
		(opts.targetCIIntegrationID != 0 || strings.TrimSpace(opts.targetRemoteGitRepoID) != "") {
		return errors.New("a server export contains multiple source repositories; use --target-repository-map for each app instead of one shared Git target, or use --skip-code")
	}
	return nil
}

func artifactPaths(sourceID, planPath, statePath string) (string, string, error) {
	if strings.TrimSpace(sourceID) == "" {
		return "", "", errors.New("source UUID is required")
	}
	if strings.TrimSpace(planPath) == "" {
		planPath = "wodby1-" + sourceID + ".migration-plan.json"
	}
	if strings.TrimSpace(statePath) == "" {
		dir := filepath.Dir(planPath)
		statePath = filepath.Join(dir, "wodby1-"+sourceID+".migration-state.json")
	}
	planPath = filepath.Clean(planPath)
	statePath = filepath.Clean(statePath)
	same, err := sameArtifactPath(planPath, statePath)
	if err != nil {
		return "", "", err
	}
	if same {
		return "", "", errors.New("--plan-file and --state-file must resolve to different paths")
	}
	sameLock, err := sameArtifactPath(planPath, statePath+".lock")
	if err != nil {
		return "", "", err
	}
	if sameLock {
		return "", "", errors.New("--plan-file must not resolve to the migration state lock path")
	}
	return planPath, statePath, nil
}

func serverAppStatePath(basePath string, sourceAppUUID string) string {
	digest := sha256.Sum256([]byte(sourceAppUUID))
	suffix := fmt.Sprintf(".app-%x", digest[:8])
	ext := filepath.Ext(basePath)
	if ext == "" {
		return basePath + suffix
	}
	return strings.TrimSuffix(basePath, ext) + suffix + ext
}

func validateServerArtifactPaths(planPath string, statePath string, export wodby1.Export) error {
	seen := map[string]string{}
	for _, app := range export.AppExports() {
		childPath := serverAppStatePath(statePath, app.App.UUID)
		same, err := sameArtifactPath(planPath, childPath)
		if err != nil {
			return err
		}
		if same {
			return errors.Errorf("--plan-file must not resolve to the per-app migration state path for source app %q", app.App.UUID)
		}
		canonical, err := canonicalArtifactPath(childPath)
		if err != nil {
			return err
		}
		if existing, duplicate := seen[canonical]; duplicate {
			return errors.Errorf("source apps %q and %q resolve to the same migration state path", existing, app.App.UUID)
		}
		seen[canonical] = app.App.UUID
	}
	return nil
}

func sameArtifactPath(first string, second string) (bool, error) {
	firstCanonical, err := canonicalArtifactPath(first)
	if err != nil {
		return false, err
	}
	secondCanonical, err := canonicalArtifactPath(second)
	if err != nil {
		return false, err
	}
	if firstCanonical == secondCanonical {
		return true, nil
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	if firstErr == nil && secondErr == nil {
		return os.SameFile(firstInfo, secondInfo), nil
	}
	if firstErr != nil && !errors.Is(firstErr, fs.ErrNotExist) {
		return false, firstErr
	}
	if secondErr != nil && !errors.Is(secondErr, fs.ErrNotExist) {
		return false, secondErr
	}
	return false, nil
}

func canonicalArtifactPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.WithStack(err)
	}
	current := absolute
	missing := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errors.WithStack(err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolute), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func stateBackedTargetApp(
	statePath string,
	plan wodby1.Plan,
) (targetID int, allowRecovery bool, err error) {
	identity := wodby1.MigrationStateIdentity{
		Source: wodby1.MigrationStateSourceIdentity{
			Kind:         plan.Source.Kind,
			ID:           plan.Source.ID,
			ConfigDigest: plan.Source.ConfigDigest,
		},
		PlanHash: plan.PlanHash,
		Target: wodby1.MigrationStateTarget{
			OrgID:     plan.Target.OrgID,
			ProjectID: plan.Target.ProjectID,
			ClusterID: plan.Target.ClusterID,
		},
	}
	state, err := wodby1.LoadMigrationState(statePath, identity)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, errors.Wrap(err, "load migration state before target preflight")
	}
	if state.App.TargetID > 0 {
		return state.App.TargetID, false, nil
	}
	operation, found := state.App.Operations["create"]
	if !found {
		return 0, false, nil
	}
	switch operation.Status {
	case wodby1.MigrationOperationIntent,
		wodby1.MigrationOperationAccepted,
		wodby1.MigrationOperationAmbiguous:
		return 0, true, nil
	default:
		return 0, false, nil
	}
}

func printPlan(cmd *cobra.Command, plan wodby1.Plan, planPath string) error {
	if plan.Status == "blocked" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Plan written to %s; resolve blocking review items before prepare.\n", planPath)
	}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(plan))
	}
	wodby1.PrintReview(cmd.OutOrStdout(), plan)
	fmt.Fprintf(cmd.OutOrStdout(), "\nPlan file: %s\n", planPath)
	return nil
}

func printPhaseResult(
	cmd *cobra.Command,
	plan wodby1.Plan,
	planPath string,
	statePath string,
	result wodby1.MigrationPhaseResult,
) error {
	output := phaseOutput{
		Phase:     result.Phase,
		PlanHash:  plan.PlanHash,
		StateFile: statePath,
		Status:    result.State.Status,
	}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(output))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Migration phase %s completed.\n", phaseLabel(result.Phase))
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", result.State.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "Plan hash: %s\n", plan.PlanHash)
	fmt.Fprintf(cmd.OutOrStdout(), "Plan file: %s\n", planPath)
	fmt.Fprintf(cmd.OutOrStdout(), "State file: %s\n", statePath)
	return nil
}

func printServerPhaseResult(
	cmd *cobra.Command,
	plan wodby1.Plan,
	planPath string,
	phase wodby1.MigrationPhase,
	apps []serverAppPhaseOutput,
) error {
	output := serverPhaseOutput{Phase: phase, PlanHash: plan.PlanHash, Apps: apps}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(output))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Server migration phase %s completed for %d app(s).\n", phaseLabel(phase), len(apps))
	fmt.Fprintf(cmd.OutOrStdout(), "Plan hash: %s\n", plan.PlanHash)
	fmt.Fprintf(cmd.OutOrStdout(), "Plan file: %s\n", planPath)
	for _, app := range apps {
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"App %s (%s): status=%s state=%s\n",
			app.Name,
			app.SourceAppUUID,
			app.Status,
			app.StateFile,
		)
	}
	return nil
}

func planOutputJSON(cmd *cobra.Command) bool {
	flag := cmd.Flags().Lookup("output")
	return flag != nil && flag.Value.String() == "json"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writePlanFile(path string, plan wodby1.Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return errors.WithStack(err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.WithStack(err)
	}
	if err := file.Close(); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.Rename(tempPath, path))
}
