package migrate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
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
	apply         bool
	verify        bool
	restart       bool
	yes           bool

	targetProject    string
	targetCluster    string
	targetApp        string
	targetEnvMap     []string
	targetStackID    int
	targetStackMap   []string
	targetServiceMap []string
	targetVersionMap []string
	targetImportMap  []string

	targetCIIntegrationID  int
	targetGitIntegrationID int
	targetRepositoryName   string
	targetRepositoryMap    []string
	targetCodeService      string
	targetGitRef           string
	targetGitRefType       string

	skipCode               bool
	skipData               bool
	sourceBackups          []string
	allowUnsupportedDrupal bool
	addMissingServices     bool
	excludeApps            []string
	excludeInstances       []string

	stateFile      string
	pollInterval   time.Duration
	waitTimeout    time.Duration
	retryAmbiguous string
	output         string
}

type migrationOutput struct {
	Action    string      `json:"action"`
	Plan      wodby1.Plan `json:"plan"`
	PlanFile  string      `json:"planFile"`
	StateFile string      `json:"stateFile"`
	Status    string      `json:"status"`
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
	Plan     wodby1.Plan                `json:"plan"`
	PlanFile string                     `json:"planFile"`
	Apps     []serverAppMigrationOutput `json:"apps"`
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
	cmd.AddCommand(newWodby1InstanceCommand(), newWodby1AppCommand(), newWodby1ServerCommand())
	return cmd
}

func newWodby1InstanceCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "instance SOURCE_INSTANCE_UUID",
		Short: "Preview, apply, or verify a Wodby 1 instance migration",
		Long: `Migrate exactly one Wodby 1 app instance. The default command is a
read-only preview. Add --apply to create the target and import data, then test
the target using its technical route. Change DNS only after testing succeeds,
and rerun the same command with --verify to validate the completed migration.

The migration exports only the selected
instance and its parent app metadata, then creates a new Wodby 2 app containing
that instance. Pass --target-app to add it to an existing Wodby 2 app instead;
the CLI infers the app's shared stack when unambiguous and never adopts or
overwrites an existing app instance. By default it imports the newest successful backup file for each
required component shown in the preview. Use --source-backup BACKUP_UUID to pin
all components to a different complete snapshot, or
--skip-data to omit data. Changes made after the selected backup completed are
not migrated. Apply is resumable and stores its plan and state in the system
temporary directory. When state exists, the same --apply command preserves the saved plan
and continues completed work. --restart replaces it only when state proves that
no target mutation occurred.`,
		Example: `  export WODBY1_SOURCE_TOKEN=...
  export WODBY_API_KEY=...

  wodby migrate wodby1 instance INSTANCE_UUID --target-cluster CLUSTER [target and mapping options]
  wodby migrate wodby1 instance INSTANCE_UUID --target-app APP --target-cluster CLUSTER [target and mapping options]
  wodby migrate wodby1 instance INSTANCE_UUID --target-cluster CLUSTER [same options] --apply
  wodby migrate wodby1 instance INSTANCE_UUID --target-cluster CLUSTER [same options] --verify`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1Instance(cmd, args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	cmd.Flags().StringVar(&opts.targetApp, "target-app", "", "Existing Wodby 2 app ID or exact name to receive this instance")
	cmd.Flags().Lookup("source-backup").Usage = "Select a successful Wodby 1 BACKUP_UUID for this instance"
	return cmd
}

func newWodby1ServerCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "server SOURCE_SERVER_UUID",
		Short: "Preview, apply, or verify every app migration from a Wodby 1 server",
		Long: `Migrate every application hosted on one Wodby 1 server. The default
command is a read-only preview; --apply performs the resumable migrations and
--verify validates them after testing and DNS cutover. Each application has an
isolated resume-state file and is processed sequentially. Runtime failures are
reported per app while remaining apps continue. A shared
--target-git-integration-id searches that integration for every source
repository by name; use --target-repository-map for per-app integrations or
repository-name overrides. A missing repository match falls back to unlinked
Custom CI. Use --skip-code only to omit code migration intentionally.
By default, the newest successful file for each required backup component is selected.
Use repeatable --source-backup APP/INSTANCE=BACKUP_UUID values to override
individual snapshots, or --skip-data to omit data. Changes after each selected
backup completed are not migrated. Test the
migrated apps before changing DNS, then use --verify. When per-app state exists,
--apply preserves the aggregate saved plan and continues completed work;
--restart is allowed only before any target mutation.`,
		Example: `  export WODBY1_SOURCE_TOKEN=...
  export WODBY_API_KEY=...

  wodby migrate wodby1 server SERVER_UUID --target-cluster CLUSTER [target and mapping options]
  wodby migrate wodby1 server SERVER_UUID --target-cluster CLUSTER [same options] --apply
  wodby migrate wodby1 server SERVER_UUID --target-cluster CLUSTER [same options] --verify`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1Server(cmd, args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	cmd.Flags().Lookup("source-backup").Usage = "Select a successful backup by APP/INSTANCE=BACKUP_UUID or INSTANCE_UUID=BACKUP_UUID (repeatable)"
	cmd.Flags().StringArrayVar(
		&opts.excludeApps,
		"exclude-app",
		nil,
		"Exclude an app by exact UUID or name (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.excludeInstances,
		"exclude-instance",
		nil,
		"Exclude an instance by UUID or APP/INSTANCE (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.targetRepositoryMap,
		"target-repository-map",
		nil,
		"Per-app Git target (APP=GIT_INTEGRATION_ID[:REPOSITORY_NAME[:SERVICE]])",
	)
	cmd.Flags().Lookup("state-file").Usage = "Base path for secure per-app resume-state files (defaults to the system temporary directory)"
	return cmd
}

func newWodby1AppCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "app SOURCE_APP_UUID",
		Short: "Preview, apply, or verify a Wodby 1 application migration",
		Long: `Migrate one Wodby 1 application. The default command is a read-only
preview. Add --apply to create and populate the target, test it using its
technical route, and change DNS only after testing succeeds. Rerun the same
command with --verify after DNS cutover. By default, the newest successful file
for each required backup component is selected. Use repeatable --source-backup
INSTANCE=BACKUP_UUID values to pin all components to explicit complete
snapshots, or --skip-data to omit data.
Changes after each selected backup completed are not migrated. If a target mutation is ambiguous, inspect Wodby 2 and pass
--retry-ambiguous only with the exact operation ID printed by the command. When
state exists, --apply preserves the saved plan and continues completed work;
--restart is allowed only before any target mutation.`,
		Example: `  export WODBY1_SOURCE_TOKEN=...
  export WODBY_API_KEY=...

  wodby migrate wodby1 app APP_UUID --target-cluster CLUSTER [target and mapping options]
  wodby migrate wodby1 app APP_UUID --target-cluster CLUSTER [same options] --apply
  wodby migrate wodby1 app APP_UUID --target-cluster CLUSTER [same options] --verify`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1App(cmd, args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	cmd.Flags().Lookup("source-backup").Usage = "Select a successful backup by INSTANCE=BACKUP_UUID or INSTANCE_UUID=BACKUP_UUID (repeatable)"
	cmd.Flags().StringArrayVar(
		&opts.excludeInstances,
		"exclude-instance",
		nil,
		"Exclude an instance by exact UUID or name (repeatable)",
	)
	return cmd
}

func defaultOptions() *options {
	return &options{
		sourceBaseURL:      defaultSourceBaseURL,
		pollInterval:       2 * time.Second,
		waitTimeout:        30 * time.Minute,
		output:             "text",
		addMissingServices: true,
	}
}

func bindFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.sourceBaseURL, "source-base-url", defaultSourceBaseURL, "Wodby 1 API base URL")
	cmd.Flags().StringVar(&opts.sourceToken, "source-token", "", "Wodby 1 API token (defaults to "+sourceTokenEnv+")")
	cmd.Flags().BoolVar(&opts.apply, "apply", false, "Create the target and import data using the displayed plan")
	cmd.Flags().BoolVar(&opts.verify, "verify", false, "Verify the applied migration after testing and DNS cutover")
	cmd.Flags().BoolVar(&opts.restart, "restart", false, "Start a new applied plan only when saved state proves that no target mutation occurred (requires --apply)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Approve a new migration plan without an interactive prompt")

	cmd.Flags().StringVar(&opts.targetProject, "target-project", "", "Wodby 2 project ID or exact name (defaults to a project-owned cluster's owner; otherwise organization-owned)")
	cmd.Flags().StringVar(&opts.targetCluster, "target-cluster", "", "Wodby 2 cluster ID or exact name")
	cmd.Flags().StringArrayVar(&opts.targetEnvMap, "target-env-map", nil, "Source-to-target environment mapping (SOURCE=TARGET)")
	cmd.Flags().IntVar(&opts.targetStackID, "target-stack-id", 0, "Existing Wodby 2 stack ID override (managed Drupal/WordPress apps get a new catalog stack by default)")
	cmd.Flags().StringArrayVar(&opts.targetStackMap, "target-stack-map", nil, "Source-to-target stack ID mapping ([APP/][INSTANCE/]SOURCE=TARGET_STACK_ID)")
	cmd.Flags().StringArrayVar(&opts.targetServiceMap, "target-service-map", nil, "Source-to-target service mapping ([APP/][INSTANCE/]SOURCE=TARGET)")
	cmd.Flags().StringArrayVar(&opts.targetVersionMap, "target-version-map", nil, "Source-service version override ([APP/][INSTANCE/]SOURCE_SERVICE=TARGET_VERSION)")
	cmd.Flags().StringArrayVar(&opts.targetImportMap, "target-import-map", nil, "Backup-to-import mapping ([APP/][INSTANCE/]COMPONENT=SERVICE:IMPORT)")

	cmd.Flags().IntVar(&opts.targetCIIntegrationID, "target-ci-integration-id", 0, "Wodby 2 CI integration ID override (otherwise Wodby CI with a linked repository, or automatic Custom CI fallback)")
	cmd.Flags().IntVar(&opts.targetGitIntegrationID, "target-git-integration-id", 0, "Optional Wodby 2 Git integration ID used to find and link the source repository")
	cmd.Flags().StringVar(&opts.targetRepositoryName, "target-repository-name", "", "Exact repository name in the selected Wodby 2 Git integration (defaults to the name derived from the Wodby 1 repository URL)")
	cmd.Flags().StringVar(&opts.targetCodeService, "target-code-service", "", "Target connect-build service name when the stack has more than one")
	cmd.Flags().StringVar(&opts.targetGitRef, "target-git-ref", "", "Git branch, tag, or commit to build (defaults to the source ref)")
	cmd.Flags().StringVar(&opts.targetGitRefType, "target-git-ref-type", "", "Git ref type: branch, tag, or commit")

	cmd.Flags().BoolVar(&opts.skipCode, "skip-code", false, "Intentionally omit repository/build-source migration")
	cmd.Flags().BoolVar(&opts.skipData, "skip-data", false, "Intentionally omit database and files imports")
	cmd.Flags().StringArrayVar(&opts.sourceBackups, "source-backup", nil, "Select a successful Wodby 1 backup (BACKUP_UUID for an instance, INSTANCE=BACKUP_UUID for an app, APP/INSTANCE=BACKUP_UUID for a server; repeatable)")
	cmd.Flags().BoolVar(
		&opts.allowUnsupportedDrupal,
		"allow-unsupported-drupal",
		false,
		"Allow Drupal 8/9 when the nginx preset does not identify Drupal 10 or newer and you have independently confirmed the application was upgraded",
	)
	cmd.Flags().BoolVar(
		&opts.addMissingServices,
		"add-missing-services",
		true,
		"Add mapped services missing from the target stack when an exact Wodby 2 service is available",
	)

	cmd.Flags().StringVar(&opts.stateFile, "state-file", "", "Secure resume-state path (defaults to the system temporary directory)")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", 2*time.Second, "Target operation polling interval")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Timeout for each target operation")
	cmd.Flags().StringVar(&opts.retryAmbiguous, "retry-ambiguous", "", "Retry exactly one inspected ambiguous operation ID")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "Output format: text or json")
}

func runWodby1App(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	return runWodby1Single(cmd, "app", sourceID, opts)
}

func runWodby1Instance(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	return runWodby1Single(cmd, "instance", sourceID, opts)
}

func runWodby1Single(cmd *cobra.Command, sourceKind string, sourceID string, opts *options) (runErr error) {
	opts.sourceToken = firstNonBlank(opts.sourceToken, os.Getenv(sourceTokenEnv))
	if err := validateOptions(opts); err != nil {
		return err
	}
	var err error
	opts.sourceToken, err = wodby1.NormalizeSourceToken(opts.sourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseTargetStackMapping(opts.targetStackMap)
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.targetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	versionMap, err := parseMapping(opts.targetVersionMap, "--target-version-map")
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

	planPath, statePath, err := artifactPaths(sourceKind, sourceID, opts.stateFile)
	if err != nil {
		return err
	}
	// From this point onward, failures describe migration state, source or target
	// API behavior, or review blockers. Reprinting command usage for those errors
	// makes them look like invalid arguments and obscures the actionable output.
	cmd.Root().SilenceUsage = true

	var reviewedPlan *wodby1.Plan
	var restartStateIdentity *wodby1.MigrationStateIdentity
	var restartStateWithMutations *wodby1.MigrationState
	var resumeState *wodby1.MigrationState
	var incompatiblePlan *wodby1.UnsupportedMigrationPlanSchemaError
	restartDeletedTargetAppID := 0
	allowedTargetAppID := 0
	allowTargetAppRecovery := false
	stateExists, err := artifactExists(statePath)
	if err != nil {
		return err
	}
	planExists, err := artifactExists(planPath)
	if err != nil {
		return err
	}
	if opts.verify && !stateExists {
		return errors.Errorf("no applied migration state found at %s; run the same command with --apply first", statePath)
	}
	if opts.restart && !stateExists {
		return errors.Errorf("--restart found no applied migration state at %s; run --apply without --restart to start a new migration", statePath)
	}
	if (opts.apply || opts.verify) && stateExists {
		if !planExists {
			return errors.Errorf(
				"applied migration state exists at %s but its plan is missing at %s; restore the plan before resuming",
				statePath,
				planPath,
			)
		}
		reviewed, err := wodby1.LoadReviewedPlan(planPath)
		if err != nil {
			if !errors.As(err, &incompatiblePlan) {
				return errors.Wrap(err, "load applied migration plan")
			}
			if incompatiblePlan.SourceKind != sourceKind || incompatiblePlan.SourceID != sourceID {
				return errors.Errorf("incompatible applied migration plan does not match the requested source %s", sourceKind)
			}
			state, stateErr := wodby1.InspectMigrationState(statePath)
			if stateErr != nil {
				return errors.Wrap(stateErr, "inspect migration state for incompatible plan")
			}
			if state.Source.Kind != sourceKind || state.Source.ID != sourceID {
				return errors.New("migration state does not match the source recorded by the incompatible plan")
			}
			if !opts.restart {
				return incompatiblePlanRestartError(incompatiblePlan, planPath, []string{statePath}, []*wodby1.MigrationState{state})
			}
			if state.CanRestartSafely() {
				identity := state.Identity()
				restartStateIdentity = &identity
			} else {
				if state.App.TargetID <= 0 {
					return incompatiblePlanUnsafeRestartError(incompatiblePlan, planPath, statePath, state)
				}
				restartStateWithMutations = state
			}
		} else {
			if reviewed.Source.Kind != sourceKind || reviewed.Source.ID != sourceID {
				return errors.Errorf("applied migration plan does not match the requested source %s", sourceKind)
			}
			state, err := wodby1.LoadMigrationState(statePath, migrationStateIdentity(reviewed))
			if err != nil {
				return errors.Wrap(err, "load migration state before target preflight")
			}
			if opts.restart {
				if state.CanRestartSafely() {
					identity := state.Identity()
					restartStateIdentity = &identity
				} else {
					if state.App.TargetID <= 0 {
						return unsafeSingleRestartError(state, statePath)
					}
					// A state with successful or ambiguous mutations normally cannot be
					// discarded. After target discovery we make one safe exception: a
					// recorded app ID that the original organization definitively says
					// no longer exists.
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
				if opts.apply && state.CanRestartSafely() {
					identity := state.Identity()
					restartStateIdentity = &identity
				}
				allowedTargetAppID, allowTargetAppRecovery = stateBackedTargetAppState(state)
				reviewedPlan = &reviewed
				resumeState = state
			}
		}
	}
	if opts.apply && resumeState != nil {
		printSingleResumeNotice(cmd, planPath, statePath, resumeState)
		if resumeState.App.TargetID > 0 {
			printSingleResumeTargetValidation(cmd, resumeState.App.TargetID)
		}
	}

	targetClient, err := wodby1.NewTargetClient(types.APIConfig{
		Endpoint: strings.TrimSpace(viper.GetString("api_base_url")),
		Key:      strings.TrimSpace(viper.GetString("api_key")),
	})
	if err != nil {
		return err
	}
	preparation := newMigrationPreparationProgress(cmd, sourceKind)
	preparation.StartStep("Discover Wodby 2 target scope")

	// Verify the caller's role in the selected destination organization before
	// asking Wodby 1 for the secret-bearing export.
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), wodby1.TargetScopeSelectors{
		Project: opts.targetProject,
		Cluster: opts.targetCluster,
	})
	if err != nil {
		return err
	}
	if restartStateWithMutations != nil {
		if restartStateWithMutations.Target.ExistingApp {
			return unsafeExistingAppRestartError(restartStateWithMutations, statePath)
		}
		if restartStateWithMutations.App.TargetID <= 0 {
			return unsafeSingleRestartError(restartStateWithMutations, statePath)
		}
		if scope.Org.ID != restartStateWithMutations.Target.OrgID {
			return errors.Errorf(
				"cannot verify deletion of saved target app ID %d because the current target resolves to organization ID %d instead of the saved organization ID %d; use the original target credentials and options",
				restartStateWithMutations.App.TargetID,
				scope.Org.ID,
				restartStateWithMutations.Target.OrgID,
			)
		}
		_, found, err := targetClient.FindAppByID(cmd.Context(), restartStateWithMutations.App.TargetID)
		if err != nil {
			return errors.Wrap(err, "verify saved target app before restart")
		}
		if found {
			if incompatiblePlan != nil {
				return incompatibleTargetAppExistsError(incompatiblePlan, planPath, statePath, restartStateWithMutations)
			}
			return unsafeSingleRestartError(restartStateWithMutations, statePath)
		}
		identity := restartStateWithMutations.Identity()
		restartStateIdentity = &identity
		restartDeletedTargetAppID = restartStateWithMutations.App.TargetID
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
		if opts.apply {
			printSingleResumeTargetValidated(cmd)
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Target %s / %s is ready.", scope.Org.Name, scope.Cluster.Name))

	var targetApp *wodby1.TargetApp
	targetStackID := opts.targetStackID
	if selector := strings.TrimSpace(opts.targetApp); selector != "" {
		preparation.StartStep("Resolve existing Wodby 2 target app and stack")
		if sourceKind != "instance" {
			return errors.New("--target-app can be used only with migrate wodby1 instance")
		}
		resolvedApp, err := targetClient.ResolveTargetApp(cmd.Context(), scope.Org.ID, selector)
		if err != nil {
			return err
		}
		if resolvedApp.ClusterApp {
			return errors.Errorf("target app %q (ID %d) is a cluster infrastructure app and cannot receive a migrated customer instance", resolvedApp.Name, resolvedApp.ID)
		}
		status := strings.ToUpper(strings.TrimSpace(resolvedApp.Status))
		if status != "OK" {
			return errors.Errorf("target app %q (ID %d) has status %q and cannot receive a migrated instance", resolvedApp.Name, resolvedApp.ID, resolvedApp.Status)
		}
		switch strings.ToLower(strings.TrimSpace(resolvedApp.OwnershipScope)) {
		case wodby1.TargetOwnershipScopeOrg:
			if scope.Project.ID > 0 {
				return errors.Errorf("target app %q (ID %d) is organization-owned, but the selected cluster is owned by project %q (ID %d); select an organization-owned cluster", resolvedApp.Name, resolvedApp.ID, scope.Project.Name, scope.Project.ID)
			}
		case wodby1.TargetOwnershipScopeProject:
			if resolvedApp.OwnerProjectID == nil || *resolvedApp.OwnerProjectID <= 0 {
				return errors.Errorf("target app %q (ID %d) did not return a valid owner project", resolvedApp.Name, resolvedApp.ID)
			}
			if scope.Project.ID == 0 {
				project, err := targetClient.GetProject(cmd.Context(), *resolvedApp.OwnerProjectID)
				if err != nil {
					return errors.Wrap(err, "resolve selected target app owner project")
				}
				if project.OrgID != scope.Org.ID {
					return errors.Errorf("target app %q (ID %d) owner project belongs to organization ID %d, expected %d", resolvedApp.Name, resolvedApp.ID, project.OrgID, scope.Org.ID)
				}
				scope.Project = project
			} else if scope.Project.ID != *resolvedApp.OwnerProjectID {
				return errors.Errorf("target app %q (ID %d) belongs to project ID %d, but the selected cluster belongs to project ID %d; select a cluster owned by the app's project", resolvedApp.Name, resolvedApp.ID, *resolvedApp.OwnerProjectID, scope.Project.ID)
			}
		default:
			return errors.Errorf("target app %q (ID %d) did not return an ownership scope; update the Wodby 2 backend before using --target-app", resolvedApp.Name, resolvedApp.ID)
		}
		instances, err := targetClient.ListAppInstances(cmd.Context(), scope.Org.ID, resolvedApp.ID)
		if err != nil {
			return err
		}
		if targetStackID == 0 {
			stackIDs := targetAppStackIDs(instances)
			switch len(stackIDs) {
			case 0:
				return errors.Errorf("target app %q (ID %d) has no app instances from which to infer a stack; provide --target-stack-id", resolvedApp.Name, resolvedApp.ID)
			case 1:
				targetStackID = stackIDs[0]
			default:
				return errors.Errorf("target app %q (ID %d) uses multiple stacks (%s); provide --target-stack-id with one of these stack IDs", resolvedApp.Name, resolvedApp.ID, joinIntValues(stackIDs))
			}
		} else if stackIDs := targetAppStackIDs(instances); len(stackIDs) != 0 && !containsInt(stackIDs, targetStackID) {
			return errors.Errorf("--target-stack-id %d is not used by target app %q (ID %d); choose one of %s", targetStackID, resolvedApp.Name, resolvedApp.ID, joinIntValues(stackIDs))
		}
		targetApp = &resolvedApp
		preparation.CompleteStep(fmt.Sprintf("Existing target app %s (ID %d) will receive the new instance using stack ID %d.", resolvedApp.Name, resolvedApp.ID, targetStackID))
	}

	// The customer command transfers required credentials in memory. Wodby 1
	// independently authorizes this export using the source organization role.
	var exportSource func(context.Context, string, wodby1.SourceBackupSelection) (wodby1.Export, error)
	switch sourceKind {
	case "app":
		exportSource = sourceClient.ExportAppWithBackups
	case "instance":
		exportSource = sourceClient.ExportInstanceWithBackups
	default:
		return errors.Errorf("unsupported single migration source kind %q", sourceKind)
	}
	backupSelection := wodby1.SourceBackupSelection{}
	if reviewedPlan != nil {
		backupSelection, err = wodby1.PlanSourceBackups(*reviewedPlan)
		if err != nil {
			return err
		}
	}
	preparation.StartStep("Export and select Wodby 1 source data")
	export, err := exportSource(cmd.Context(), sourceID, backupSelection)
	if err != nil {
		return err
	}
	if reviewedPlan == nil && len(opts.sourceBackups) != 0 {
		backupSelection, err = wodby1.ResolveSourceBackups(export, sourceKind, opts.sourceBackups)
		if err != nil {
			return err
		}
		export, err = exportSource(cmd.Context(), sourceID, backupSelection)
		if err != nil {
			return err
		}
	} else if reviewedPlan != nil && len(opts.sourceBackups) != 0 {
		requested, err := wodby1.ResolveSourceBackups(export, sourceKind, opts.sourceBackups)
		if err != nil {
			return err
		}
		if !sameSourceBackupSelection(requested, backupSelection) {
			return errors.New("--source-backup does not match the snapshots pinned by the applied plan; use the original selection or restart before target changes")
		}
	}
	export, selection, err := wodby1.SelectExport(export, sourceKind, opts.excludeApps, opts.excludeInstances, opts.sourceToken)
	if err != nil {
		return err
	}
	backupSelection, err = wodby1.ExportSourceBackups(export)
	if err != nil {
		return err
	}
	preparation.CompleteStep(migrationExportSummary(export))

	preparation.StartStep("Resolve target environment types")
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
	preparation.CompleteStep(fmt.Sprintf("Resolved %d target environment selector(s).", len(resolved)))

	preparation.StartStep("Build migration plan")
	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:                    sourceKind,
		SourceID:                      sourceID,
		TargetOrg:                     scope.Org.Name,
		TargetProject:                 opts.targetProject,
		TargetCluster:                 opts.targetCluster,
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
		TargetCIIntegrationID:         opts.targetCIIntegrationID,
		Repository: wodby1.RepositoryTargetPlan{
			GitIntegrationID: opts.targetGitIntegrationID,
			RepositoryName:   strings.TrimSpace(opts.targetRepositoryName),
			Service:          strings.TrimSpace(opts.targetCodeService),
		},
		SkipCode:               opts.skipCode,
		SkipData:               opts.skipData,
		RequireData:            !opts.skipData,
		AllowUnsupportedDrupal: opts.allowUnsupportedDrupal,
		Selection:              &selection,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := wodby1.PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Planned %d app(s) and %d instance(s).", plan.Summary.Apps, plan.Summary.Instances))
	preparation.StartStep("Inspect target mappings and capabilities")
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, wodby1.TargetPreflightOptions{
		SkipCode:                    opts.skipCode,
		SkipData:                    opts.skipData,
		GitRef:                      opts.targetGitRef,
		GitRefType:                  opts.targetGitRefType,
		CodeService:                 opts.targetCodeService,
		AllowedTargetAppID:          allowedTargetAppID,
		AllowStateBackedAppRecovery: allowTargetAppRecovery,
		AllowedTargetInstanceIDs:    stateTargetInstanceIDs(resumeState),
		StateBackedInstanceRecovery: stateTargetInstanceRecovery(resumeState),
		AddMissingServices:          opts.addMissingServices,
		Progress:                    preparation.TargetPreflight,
	})
	if err != nil {
		return err
	}
	preparation.CompleteStep("Target preflight completed.")

	if !opts.apply && !opts.verify {
		return printPreview(cmd, plan, prepared)
	}
	if plan.Summary.Blocking != 0 {
		action := "apply"
		if opts.verify {
			action = "verify"
		}
		return rejectBlockedMigrationAction(cmd, plan, action, prepared)
	}
	if reviewedPlan != nil {
		if err := wodby1.RestoreReviewedPlanForResume(&plan, *reviewedPlan); err != nil {
			return errors.Wrap(err, "cannot continue from saved plan because the current source or options change its executable actions; the saved plan was not overwritten")
		}
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
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
	if opts.apply {
		if resumeState == nil && opts.restart {
			if restartDeletedTargetAppID > 0 {
				printDeletedTargetRestartNotice(cmd, planPath, statePath, restartDeletedTargetAppID)
			} else {
				printRestartNotice(cmd, planPath, statePath)
			}
		}
		if err := printApplyReview(cmd, plan, resumeState != nil, prepared); err != nil {
			return err
		}
		if resumeState == nil {
			if err := confirmApply(cmd, opts.yes); err != nil {
				return err
			}
		}
		if restartStateIdentity != nil {
			var err error
			if restartDeletedTargetAppID > 0 {
				err = wodby1.RemoveMigrationStateAfterTargetDeletion(statePath, *restartStateIdentity, restartDeletedTargetAppID)
			} else {
				err = wodby1.RemoveRestartableMigrationState(statePath, *restartStateIdentity)
			}
			if err != nil {
				return errors.Wrap(err, "replace migration state for restart")
			}
		}
		if resumeState == nil {
			if err := writePlanFile(planPath, plan); err != nil {
				return err
			}
			printArtifactNotice(cmd, planPath, statePath)
		}
	}

	executor, err := wodby1.NewMigrationExecutor(targetClient, wodby1.MigrationExecutorOptions{
		StatePath:               statePath,
		PollInterval:            opts.pollInterval,
		OperationTimeout:        opts.waitTimeout,
		RetryAmbiguousOperation: opts.retryAmbiguous,
		Progress:                migrationProgressReporter(cmd),
		RefreshSource: func(ctx context.Context) (wodby1.Export, error) {
			refreshed, err := exportSource(ctx, sourceID, backupSelection)
			if err != nil {
				return wodby1.Export{}, err
			}
			return wodby1.ApplySourceSelection(refreshed, plan.Selection, opts.sourceToken)
		},
	})
	if err != nil {
		return err
	}

	var action string
	if opts.apply {
		action = "apply"
		_, err = executor.Apply(cmd.Context(), export, plan, prepared)
	} else {
		action = "verify"
		_, err = executor.Verify(cmd.Context(), export, plan, prepared, scope.Cluster)
	}
	if err != nil {
		if opts.apply {
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

func migrationProgressReporter(cmd *cobra.Command) func(string) {
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

func newMigrationPreparationProgress(cmd *cobra.Command, sourceKind string) *migrationPreparationProgress {
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

func (p *migrationPreparationProgress) TargetPreflight(event wodby1.TargetPreflightProgress) {
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

func migrationProgressWriter(cmd *cobra.Command) io.Writer {
	if planOutputJSON(cmd) {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

func migrationExportSummary(export wodby1.Export) string {
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

func runWodby1Server(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	opts.sourceToken = firstNonBlank(opts.sourceToken, os.Getenv(sourceTokenEnv))
	if err := validateOptions(opts); err != nil {
		return err
	}
	var err error
	opts.sourceToken, err = wodby1.NormalizeSourceToken(opts.sourceToken)
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	stackMap, err := parseTargetStackMapping(opts.targetStackMap)
	if err != nil {
		return err
	}
	serviceMap, err := parseMapping(opts.targetServiceMap, "--target-service-map")
	if err != nil {
		return err
	}
	versionMap, err := parseMapping(opts.targetVersionMap, "--target-version-map")
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

	planPath, statePath, err := artifactPaths("server", sourceID, opts.stateFile)
	if err != nil {
		return err
	}
	// Local command and flag validation is complete. Keep Cobra's usage output
	// for errors above this boundary, but suppress it for migration failures.
	cmd.Root().SilenceUsage = true

	var reviewedPlan *wodby1.Plan
	var restartStateIdentities map[string]wodby1.MigrationStateIdentity
	var restartDeletedTargetAppIDs map[string]int
	var incompatiblePlan *wodby1.UnsupportedMigrationPlanSchemaError
	var incompatibleServerStates map[string]*wodby1.MigrationState
	statePaths, err := serverMigrationStatePaths(statePath)
	if err != nil {
		return err
	}
	stateExists := len(statePaths) != 0
	planExists, err := artifactExists(planPath)
	if err != nil {
		return err
	}
	if opts.verify && !stateExists {
		return errors.Errorf("no applied server migration state found for %s; run the same command with --apply first", sourceID)
	}
	if opts.restart && !stateExists {
		return errors.Errorf("--restart found no applied server migration state for %s; run --apply without --restart to start a new migration", sourceID)
	}
	if (opts.apply || opts.verify) && stateExists {
		if !planExists {
			return errors.Errorf(
				"applied server migration state exists for %s but its plan is missing at %s; restore the plan before resuming",
				sourceID,
				planPath,
			)
		}
		reviewed, err := wodby1.LoadReviewedPlan(planPath)
		if err != nil {
			if !errors.As(err, &incompatiblePlan) {
				return errors.Wrap(err, "load applied migration plan")
			}
			if incompatiblePlan.SourceKind != "server" || incompatiblePlan.SourceID != sourceID {
				return errors.New("incompatible applied migration plan does not match the requested source server")
			}
			incompatibleServerStates, err = inspectIncompatibleServerMigrationStates(statePath, statePaths)
			if err != nil {
				return err
			}
			states := make([]*wodby1.MigrationState, 0, len(incompatibleServerStates))
			for _, path := range statePaths {
				states = append(states, incompatibleServerStates[path])
			}
			if !opts.restart {
				return incompatiblePlanRestartError(incompatiblePlan, planPath, statePaths, states)
			}
			restartStateIdentities = make(map[string]wodby1.MigrationStateIdentity, len(incompatibleServerStates))
			restartDeletedTargetAppIDs = map[string]int{}
			for _, path := range statePaths {
				state := incompatibleServerStates[path]
				restartStateIdentities[path] = state.Identity()
				if !state.CanRestartSafely() {
					if state.App.TargetID <= 0 {
						return incompatiblePlanUnsafeRestartError(incompatiblePlan, planPath, statePath, state)
					}
					restartDeletedTargetAppIDs[path] = state.App.TargetID
				}
			}
		} else {
			if reviewed.Source.Kind != "server" || reviewed.Source.ID != sourceID {
				return errors.New("applied migration plan does not match the requested source server")
			}
			if !opts.restart && (reviewed.Status == "blocked" || reviewed.Summary.Blocking != 0) {
				return errors.Errorf(
					"applied migration plan is blocked by %d review item(s); inspect %s",
					reviewed.Summary.Blocking,
					planPath,
				)
			}
			if opts.restart {
				identities, restartable, err := restartableServerMigrationStates(statePath, reviewed, statePaths)
				if err != nil {
					return err
				}
				if !restartable {
					return errors.Errorf(
						"cannot restart server migration from scratch because at least one resume state records target mutations; continue without --restart to reuse the saved plan and completed work",
					)
				}
				restartStateIdentities = identities
			} else {
				reviewedPlan = &reviewed
				if opts.apply {
					identities, restartable, err := restartableServerMigrationStates(statePath, reviewed, statePaths)
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
	if opts.apply && reviewedPlan != nil {
		printServerResumeNotice(cmd, planPath, statePath, len(statePaths))
	}

	targetClient, err := wodby1.NewTargetClient(types.APIConfig{
		Endpoint: strings.TrimSpace(viper.GetString("api_base_url")),
		Key:      strings.TrimSpace(viper.GetString("api_key")),
	})
	if err != nil {
		return err
	}
	preparation := newMigrationPreparationProgress(cmd, "server")
	preparation.StartStep("Discover Wodby 2 target scope")
	scope, err := targetClient.DiscoverTargetScope(cmd.Context(), wodby1.TargetScopeSelectors{
		Project: opts.targetProject,
		Cluster: opts.targetCluster,
	})
	if err != nil {
		return err
	}
	for _, path := range statePaths {
		targetAppID := restartDeletedTargetAppIDs[path]
		if targetAppID <= 0 {
			continue
		}
		state := incompatibleServerStates[path]
		if scope.Org.ID != state.Target.OrgID {
			return errors.Errorf(
				"cannot verify deletion of saved target app ID %d because the current target resolves to organization ID %d instead of the saved organization ID %d; use the original target credentials and options",
				targetAppID,
				scope.Org.ID,
				state.Target.OrgID,
			)
		}
		_, found, err := targetClient.FindAppByID(cmd.Context(), targetAppID)
		if err != nil {
			return errors.Wrap(err, "verify saved target app before restarting incompatible server migration")
		}
		if found {
			return incompatibleTargetAppExistsError(incompatiblePlan, planPath, path, state)
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Target %s / %s is ready.", scope.Org.Name, scope.Cluster.Name))

	backupSelection := wodby1.SourceBackupSelection{}
	if reviewedPlan != nil {
		backupSelection, err = wodby1.PlanSourceBackups(*reviewedPlan)
		if err != nil {
			return err
		}
	}
	preparation.StartStep("Export and select Wodby 1 source data")
	export, err := sourceClient.ExportServerWithBackups(cmd.Context(), sourceID, backupSelection)
	if err != nil {
		return err
	}
	if reviewedPlan == nil && len(opts.sourceBackups) != 0 {
		backupSelection, err = wodby1.ResolveSourceBackups(export, "server", opts.sourceBackups)
		if err != nil {
			return err
		}
		export, err = sourceClient.ExportServerWithBackups(cmd.Context(), sourceID, backupSelection)
		if err != nil {
			return err
		}
	} else if reviewedPlan != nil && len(opts.sourceBackups) != 0 {
		requested, err := wodby1.ResolveSourceBackups(export, "server", opts.sourceBackups)
		if err != nil {
			return err
		}
		if !sameSourceBackupSelection(requested, backupSelection) {
			return errors.New("--source-backup does not match the snapshots pinned by the applied plan; use the original selection or restart before target changes")
		}
	}
	export, selection, err := wodby1.SelectExport(export, "server", opts.excludeApps, opts.excludeInstances, opts.sourceToken)
	if err != nil {
		return err
	}
	backupSelection, err = wodby1.ExportSourceBackups(export)
	if err != nil {
		return err
	}
	if err := validateServerRepositoryOptions(export, opts); err != nil {
		return err
	}
	if err := validateServerArtifactPaths(planPath, statePath, export); err != nil {
		return err
	}
	preparation.CompleteStep(migrationExportSummary(export))

	preparation.StartStep("Resolve target environment types")
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
	preparation.CompleteStep(fmt.Sprintf("Resolved %d target environment selector(s).", len(resolved)))

	preparation.StartStep("Build migration plan")
	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:                    "server",
		SourceID:                      sourceID,
		TargetOrg:                     scope.Org.Name,
		TargetProject:                 opts.targetProject,
		TargetCluster:                 opts.targetCluster,
		TargetEnvMap:                  envMap,
		TargetOrgOwnerOrAdminVerified: true,
		TargetScope:                   &scope,
		TargetEnvs:                    targetEnvs,
		TargetStackID:                 opts.targetStackID,
		TargetStackMap:                stackMap,
		TargetServiceMap:              serviceMap,
		TargetVersionMap:              versionMap,
		TargetImportMap:               importMap,
		TargetCIIntegrationID:         opts.targetCIIntegrationID,
		Repository: wodby1.RepositoryTargetPlan{
			GitIntegrationID: opts.targetGitIntegrationID,
			RepositoryName:   strings.TrimSpace(opts.targetRepositoryName),
			Service:          strings.TrimSpace(opts.targetCodeService),
		},
		RepositoryByApp:        repositoryMap,
		SkipCode:               opts.skipCode,
		SkipData:               opts.skipData,
		RequireData:            !opts.skipData,
		AllowUnsupportedDrupal: opts.allowUnsupportedDrupal,
		Selection:              &selection,
	})
	if err != nil {
		return err
	}
	if reviewedPlan != nil {
		if err := wodby1.PinReviewedTargets(&plan, *reviewedPlan); err != nil {
			return err
		}
	}
	preparation.CompleteStep(fmt.Sprintf("Planned %d app(s) and %d instance(s).", plan.Summary.Apps, plan.Summary.Instances))

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
			if opts.verify {
				exists, err := artifactExists(childStatePath)
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
			targetID, allowRecovery, err := stateBackedTargetApp(childStatePath, childPlan)
			if err != nil {
				return err
			}
			allowedTargetAppIDs[app.SourceUUID] = targetID
			stateBackedRecovery[app.SourceUUID] = allowRecovery
		}
	}
	preparation.StartStep("Inspect target mappings and capabilities")
	prepared, err := targetClient.PreflightTarget(cmd.Context(), export, &plan, wodby1.TargetPreflightOptions{
		SkipCode:               opts.skipCode,
		SkipData:               opts.skipData,
		GitRef:                 opts.targetGitRef,
		GitRefType:             opts.targetGitRefType,
		CodeService:            opts.targetCodeService,
		AllowedTargetAppIDs:    allowedTargetAppIDs,
		StateBackedAppRecovery: stateBackedRecovery,
		AddMissingServices:     opts.addMissingServices,
		Progress:               preparation.TargetPreflight,
	})
	if err != nil {
		return err
	}
	preparation.CompleteStep("Target preflight completed.")

	if !opts.apply && !opts.verify {
		return printPreview(cmd, plan, prepared)
	}
	if plan.Summary.Blocking != 0 {
		action := "apply"
		if opts.verify {
			action = "verify"
		}
		return rejectBlockedMigrationAction(cmd, plan, action, prepared)
	}
	if reviewedPlan != nil {
		if err := wodby1.RestoreReviewedPlanForResume(&plan, *reviewedPlan); err != nil {
			return errors.Wrap(err, "cannot continue from saved server plan because the current source or options change its executable actions; the saved plan was not overwritten")
		}
	}
	if err := ensureArtifactDirectories(planPath, statePath); err != nil {
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
	if opts.apply {
		if reviewedPlan == nil && opts.restart {
			printRestartNotice(cmd, planPath, statePath)
		}
		if err := printApplyReview(cmd, plan, reviewedPlan != nil, prepared); err != nil {
			return err
		}
		if reviewedPlan == nil {
			if err := confirmApply(cmd, opts.yes); err != nil {
				return err
			}
		}
		if len(restartStateIdentities) != 0 {
			if err := removeServerMigrationStates(statePath, restartStateIdentities, restartDeletedTargetAppIDs); err != nil {
				return errors.Wrap(err, "restart server migration after definitive target rejection")
			}
		}
		if reviewedPlan == nil {
			if err := writePlanFile(planPath, plan); err != nil {
				return err
			}
			printServerArtifactNotice(cmd, planPath, statePath)
		}
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
			RetryAmbiguousOperation: opts.retryAmbiguous,
			Progress:                migrationProgressReporter(cmd),
			RefreshSource: func(ctx context.Context) (wodby1.Export, error) {
				refreshed, err := sourceClient.ExportServerWithBackups(ctx, sourceID, backupSelection)
				if err != nil {
					return wodby1.Export{}, err
				}
				refreshed, err = wodby1.ApplySourceSelection(refreshed, plan.Selection, opts.sourceToken)
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
	results := make([]serverAppMigrationOutput, 0, len(executions))
	failedApps := 0
	serverAction := "verification"
	if opts.apply {
		serverAction = "migration"
	}
	progressWriter := migrationProgressWriter(cmd)
	for executionIndex, execution := range executions {
		fmt.Fprintf(
			progressWriter,
			"\n%s\n",
			cliColor(progressWriter, cliColorBold+cliColorCyan, fmt.Sprintf("Server %s: app %d/%d: %s", serverAction, executionIndex+1, len(executions), execution.name)),
		)
		if opts.apply {
			_, err = execution.executor.Apply(cmd.Context(), execution.export, execution.plan, execution.prepared)
		} else {
			_, err = execution.executor.Verify(cmd.Context(), execution.export, execution.plan, execution.prepared, scope.Cluster)
		}
		if err != nil {
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
			Status:        publicMigrationStatus(opts.apply),
		})
		fmt.Fprintf(
			progressWriter,
			"%s\n",
			cliColor(progressWriter, cliColorGreen, fmt.Sprintf("App %d/%d completed: %s", executionIndex+1, len(executions), execution.name)),
		)
	}
	action := "verify"
	if opts.apply {
		action = "apply"
	}
	if err := printServerMigrationResult(cmd, action, plan, planPath, results); err != nil {
		return err
	}
	if failedApps != 0 {
		return errors.Errorf("server migration completed with %d failed app(s); inspect the result above and rerun the same command to resume", failedApps)
	}
	return nil
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
	if opts.apply && opts.verify {
		return errors.New("--apply and --verify cannot be used together")
	}
	if opts.restart && !opts.apply {
		return errors.New("--restart requires --apply")
	}
	if len(opts.sourceBackups) != 0 && opts.skipData {
		return errors.New("--source-backup cannot be used with --skip-data")
	}
	if strings.TrimSpace(opts.targetCluster) == "" {
		return errors.New("--target-cluster is required")
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
	if opts.targetCIIntegrationID < 0 {
		return errors.New("--target-ci-integration-id cannot be negative")
	}
	if opts.targetGitIntegrationID < 0 {
		return errors.New("--target-git-integration-id cannot be negative")
	}
	if opts.targetStackID < 0 {
		return errors.New("--target-stack-id must be a positive ID")
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

func targetAppStackIDs(instances []wodby1.TargetAppInstance) []int {
	seen := map[int]bool{}
	for _, instance := range instances {
		if instance.StackID > 0 {
			seen[instance.StackID] = true
		}
	}
	result := make([]int, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Ints(result)
	return result
}

func containsInt(values []int, expected int) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func joinIntValues(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
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

func sameSourceBackupSelection(left, right wodby1.SourceBackupSelection) bool {
	if len(left) != len(right) {
		return false
	}
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

func parseRepositoryMapping(values []string) (map[string]wodby1.RepositoryTargetPlan, error) {
	result := map[string]wodby1.RepositoryTargetPlan{}
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
		mapping := wodby1.RepositoryTargetPlan{
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
		strings.TrimSpace(opts.targetRepositoryName) != "" {
		return errors.New("a server export contains multiple source repositories; use --target-repository-map for per-app repository-name overrides instead of one shared --target-repository-name, or use --skip-code")
	}
	return nil
}

func artifactPaths(sourceKind, sourceID, statePath string) (string, string, error) {
	sourceKind = strings.ToLower(strings.TrimSpace(sourceKind))
	if sourceKind != "instance" && sourceKind != "app" && sourceKind != "server" {
		return "", "", errors.Errorf("unsupported migration source kind %q", sourceKind)
	}
	if strings.TrimSpace(sourceID) == "" {
		return "", "", errors.New("source UUID is required")
	}
	for _, char := range sourceID {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return "", "", errors.New("source UUID contains unsupported characters")
	}
	dir := defaultMigrationArtifactDir()
	base := "wodby1-" + sourceKind + "-" + sourceID
	planPath := filepath.Join(dir, base+".migration-plan.json")
	if strings.TrimSpace(statePath) == "" {
		statePath = filepath.Join(dir, base+".migration-state.json")
	}
	planPath = filepath.Clean(planPath)
	statePath = filepath.Clean(statePath)
	same, err := sameArtifactPath(planPath, statePath)
	if err != nil {
		return "", "", err
	}
	if same {
		return "", "", errors.New("--state-file must not resolve to the temporary migration plan path")
	}
	sameLock, err := sameArtifactPath(planPath, statePath+".lock")
	if err != nil {
		return "", "", err
	}
	if sameLock {
		return "", "", errors.New("temporary migration plan must not resolve to the migration state lock path")
	}
	return planPath, statePath, nil
}

func defaultMigrationArtifactDir() string {
	return filepath.Join(os.TempDir(), "wodby-migrations")
}

func ensureArtifactDirectories(planPath, statePath string) error {
	defaultDir := defaultMigrationArtifactDir()
	info, err := os.Lstat(defaultDir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(defaultDir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return errors.Wrap(err, "create temporary Wodby migration directory")
		}
		info, err = os.Lstat(defaultDir)
	}
	if err != nil {
		return errors.WithStack(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.Errorf("temporary Wodby migration directory %s must be a private directory with no group or other permissions", defaultDir)
	}
	for _, dir := range []string{filepath.Dir(planPath), filepath.Dir(statePath)} {
		if dir == defaultDir {
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.Wrap(err, "create migration artifact directory")
		}
	}
	return nil
}

func artifactExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, errors.WithStack(err)
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

// serverMigrationStatePaths returns every per-app state reserved by a server
// migration. The aggregate state path itself is intentionally never written.
func serverMigrationStatePaths(basePath string) ([]string, error) {
	dir := filepath.Dir(basePath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.WithStack(err)
	}

	filename := filepath.Base(basePath)
	ext := filepath.Ext(filename)
	prefix := strings.TrimSuffix(filename, ext) + ".app-"
	paths := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		digest := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		if len(digest) != 16 {
			continue
		}
		if _, err := hex.DecodeString(digest); err == nil {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths, nil
}

func restartableServerMigrationStates(
	basePath string,
	plan wodby1.Plan,
	paths []string,
) (map[string]wodby1.MigrationStateIdentity, bool, error) {
	appIDs := make(map[string]bool, len(plan.Apps))
	for _, app := range plan.Apps {
		appIDs[app.SourceUUID] = true
	}
	identities := make(map[string]wodby1.MigrationStateIdentity, len(paths))
	for _, path := range paths {
		state, err := wodby1.InspectMigrationState(path)
		if err != nil {
			return nil, false, errors.Wrap(err, "inspect server app migration state")
		}
		if state.Source.Kind != "app" || !appIDs[state.Source.ID] {
			return nil, false, errors.Errorf(
				"server migration state %s does not belong to an app in the applied plan",
				path,
			)
		}
		expectedPath := serverAppStatePath(basePath, state.Source.ID)
		same, err := sameArtifactPath(path, expectedPath)
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

func inspectIncompatibleServerMigrationStates(
	basePath string,
	paths []string,
) (map[string]*wodby1.MigrationState, error) {
	states := make(map[string]*wodby1.MigrationState, len(paths))
	seenApps := make(map[string]bool, len(paths))
	var target *wodby1.MigrationStateTarget
	for _, path := range paths {
		state, err := wodby1.InspectMigrationState(path)
		if err != nil {
			return nil, errors.Wrap(err, "inspect server app migration state for incompatible plan")
		}
		if state.Source.Kind != "app" || state.Source.ID == "" || seenApps[state.Source.ID] {
			return nil, errors.Errorf("server migration state %s has an invalid source app identity", path)
		}
		seenApps[state.Source.ID] = true
		expectedPath := serverAppStatePath(basePath, state.Source.ID)
		same, err := sameArtifactPath(path, expectedPath)
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
	basePath string,
	expected map[string]wodby1.MigrationStateIdentity,
	deletedTargetAppIDs map[string]int,
) error {
	paths, err := serverMigrationStatePaths(basePath)
	if err != nil {
		return err
	}
	if len(paths) != len(expected) {
		return wodby1.ErrMigrationStateConcurrentUpdate
	}
	for _, path := range paths {
		identity, exists := expected[path]
		if !exists {
			return wodby1.ErrMigrationStateConcurrentUpdate
		}
		var err error
		if targetAppID := deletedTargetAppIDs[path]; targetAppID > 0 {
			err = wodby1.RemoveMigrationStateAfterTargetDeletion(path, identity, targetAppID)
		} else {
			err = wodby1.RemoveRestartableMigrationState(path, identity)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
			return errors.Errorf("temporary plan file must not resolve to the per-app migration state path for source app %q", app.App.UUID)
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
	state, err := wodby1.LoadMigrationState(statePath, migrationStateIdentity(plan))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, errors.Wrap(err, "load migration state before target preflight")
	}
	targetID, allowRecovery = stateBackedTargetAppState(state)
	return targetID, allowRecovery, nil
}

func migrationStateIdentity(plan wodby1.Plan) wodby1.MigrationStateIdentity {
	return wodby1.MigrationStateIdentity{
		Source: wodby1.MigrationStateSourceIdentity{
			Kind:         plan.Source.Kind,
			ID:           plan.Source.ID,
			ConfigDigest: plan.Source.ConfigDigest,
		},
		PlanHash: plan.PlanHash,
		Target: wodby1.MigrationStateTarget{
			OrgID:       plan.Target.OrgID,
			ProjectID:   plan.Target.ProjectID,
			ClusterID:   plan.Target.ClusterID,
			AppID:       plan.Target.AppID,
			ExistingApp: plan.Target.AppID > 0,
		},
	}
}

func incompatiblePlanRestartError(
	plan *wodby1.UnsupportedMigrationPlanSchemaError,
	planPath string,
	statePaths []string,
	states []*wodby1.MigrationState,
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
		fmt.Fprintf(&message, "\nExisting target app ID(s): %s", strings.Join(existingTargetIDs, ", "))
		fmt.Fprintf(&message, "\nThe CLI will not delete or discard state containing changes to an existing app. Continue with the CLI version that created this migration state.")
		return errors.New(message.String())
	}
	if len(targetIDs) == 0 {
		fmt.Fprintf(&message, "\nNext step: rerun the same command with --apply --restart. The CLI will replace the obsolete plan and state files.")
	} else {
		fmt.Fprintf(&message, "\nSaved target app ID(s): %s", strings.Join(targetIDs, ", "))
		fmt.Fprintf(&message, "\nNext step: delete these target apps if they still exist, then rerun the same command with --apply --restart. The CLI will verify their deletion and replace the obsolete plan and state files.")
	}
	return errors.New(message.String())
}

func incompatiblePlanUnsafeRestartError(
	plan *wodby1.UnsupportedMigrationPlanSchemaError,
	planPath string,
	statePath string,
	state *wodby1.MigrationState,
) error {
	base := incompatiblePlanRestartError(plan, planPath, []string{statePath}, []*wodby1.MigrationState{state})
	return errors.Errorf("%s\nThe saved state records target mutations but does not identify a target app, so the CLI cannot discard it safely.", base)
}

func incompatibleTargetAppExistsError(
	plan *wodby1.UnsupportedMigrationPlanSchemaError,
	planPath string,
	statePath string,
	state *wodby1.MigrationState,
) error {
	if state.Target.ExistingApp {
		return errors.Errorf(
			"the saved migration uses incompatible plan schema %s (supported: %s) and records changes while adding an instance to existing target app ID %d\nPlan: %s\nState: %s\nThe CLI will not ask you to delete an existing app; continue with the CLI version that created this migration state",
			plan.Actual,
			plan.Supported,
			state.Target.AppID,
			planPath,
			statePath,
		)
	}
	return errors.Errorf(
		"the saved migration uses incompatible plan schema %s (supported: %s) and cannot be resumed\nPlan: %s\nState: %s\nTarget app ID %d still exists. Delete it, then rerun the same command with --apply --restart; the CLI will replace the obsolete plan and state files",
		plan.Actual,
		plan.Supported,
		planPath,
		statePath,
		state.App.TargetID,
	)
}

func unsafeSingleRestartError(state *wodby1.MigrationState, statePath string) error {
	if state == nil {
		return errors.Errorf("cannot restart from scratch because migration state %s is unavailable", statePath)
	}
	target := ""
	if state.App.TargetID > 0 {
		target = fmt.Sprintf(" (target app ID %d)", state.App.TargetID)
	}
	return errors.Errorf(
		"cannot restart migration from scratch because state %s records target mutations%s at status=%s phase=%s; continue without --restart to reuse the saved plan and completed work",
		statePath,
		target,
		state.Status,
		state.Phase,
	)
}

func unsafeExistingAppRestartError(state *wodby1.MigrationState, statePath string) error {
	return errors.Errorf(
		"cannot restart this migration from scratch because state %s records changes made while adding an instance to existing target app ID %d; the CLI will never ask you to delete an existing app. Continue without --restart to reuse completed work",
		statePath,
		state.Target.AppID,
	)
}

func stateBackedTargetAppState(state *wodby1.MigrationState) (targetID int, allowRecovery bool) {
	if state.App.TargetID > 0 {
		return state.App.TargetID, false
	}
	operation, found := state.App.Operations["create"]
	if !found {
		return 0, false
	}
	switch operation.Status {
	case wodby1.MigrationOperationIntent,
		wodby1.MigrationOperationAccepted,
		wodby1.MigrationOperationAmbiguous:
		return 0, true
	default:
		return 0, false
	}
}

func stateTargetInstanceIDs(state *wodby1.MigrationState) map[string]int {
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

func stateTargetInstanceRecovery(state *wodby1.MigrationState) map[string]bool {
	if state == nil {
		return nil
	}
	result := map[string]bool{}
	for sourceID, instance := range state.Instances {
		if instance == nil || instance.TargetID > 0 {
			continue
		}
		operation, exists := instance.Operations["create"]
		if exists && (operation.Status == wodby1.MigrationOperationIntent || operation.Status == wodby1.MigrationOperationAmbiguous) {
			result[sourceID] = true
		}
	}
	return result
}

func printPreview(cmd *cobra.Command, plan wodby1.Plan, prepared ...wodby1.PreparedMigration) error {
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(plan))
	}
	wodby1.PrintReview(cmd.OutOrStdout(), plan, prepared...)
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

func printApplyReview(cmd *cobra.Command, plan wodby1.Plan, continuing bool, prepared ...wodby1.PreparedMigration) error {
	if planOutputJSON(cmd) {
		return nil
	}
	wodby1.PrintReview(cmd.OutOrStdout(), plan, prepared...)
	if continuing {
		fmt.Fprintln(cmd.OutOrStdout(), "\nContinuing the saved migration plan shown above.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "\nApplying the migration plan shown above.")
	}
	return nil
}

func confirmApply(cmd *cobra.Command, approved bool) error {
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

func printSingleResumeNotice(
	cmd *cobra.Command,
	planPath string,
	statePath string,
	state *wodby1.MigrationState,
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
	if viper.GetBool("verbose") {
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

func printServerResumeNotice(cmd *cobra.Command, planPath string, statePath string, states int) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "Server migration resume setup")
	fmt.Fprintln(w, "\nStep 1/3: Load saved migration")
	fmt.Fprintln(w, "  Saved server plan: found")
	fmt.Fprintf(w, "  Per-app resume states: %d found\n", states)
	if viper.GetBool("verbose") {
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

func printSingleResumeTargetValidation(cmd *cobra.Command, targetAppID int) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "\nStep 4/4: Validate saved target")
	fmt.Fprintf(w, "  Target app ID: %d\n", targetAppID)
}

func printSingleResumeTargetValidated(cmd *cobra.Command) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "  Target app: found")
}

func migrationStatusLabel(status wodby1.MigrationStatus) string {
	switch status {
	case wodby1.MigrationStatusInitialized:
		return "Ready"
	case wodby1.MigrationStatusRunning:
		return "Running"
	case wodby1.MigrationStatusFailed:
		return "Stopped"
	case wodby1.MigrationStatusComplete:
		return "Complete"
	default:
		return string(status)
	}
}

func migrationPhaseLabel(phase wodby1.MigrationPhase) string {
	switch phase {
	case wodby1.MigrationPhasePlan:
		return "Planning"
	case wodby1.MigrationPhasePrepare:
		return "Target preparation"
	case wodby1.MigrationPhaseSyncData:
		return "Data import"
	case wodby1.MigrationPhaseVerify:
		return "Verification"
	default:
		return string(phase)
	}
}

func printRestartNotice(cmd *cobra.Command, planPath string, statePath string) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, "Starting the migration from scratch as requested by --restart.")
	fmt.Fprintf(w, "The safely restartable state will be replaced: %s\n", statePath)
	fmt.Fprintf(w, "The applied plan will be regenerated: %s\n", planPath)
}

func printDeletedTargetRestartNotice(cmd *cobra.Command, planPath string, statePath string, targetAppID int) {
	w := cmd.OutOrStdout()
	if planOutputJSON(cmd) {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintf(w, "Saved target app ID %d no longer exists; --restart will replace its stale migration state.\n", targetAppID)
	fmt.Fprintf(w, "The previous applied plan will be replaced: %s\n", planPath)
	fmt.Fprintf(w, "The stale resume state will be replaced: %s\n", statePath)
}

func rejectBlockedMigrationAction(cmd *cobra.Command, plan wodby1.Plan, action string, prepared ...wodby1.PreparedMigration) error {
	if !planOutputJSON(cmd) {
		wodby1.PrintReview(cmd.OutOrStdout(), plan, prepared...)
		printInstanceExclusionHints(cmd, plan)
	}
	return errors.Errorf("cannot %s migration: resolve the %d blocking review item(s) shown above", action, plan.Summary.Blocking)
}

func printInstanceExclusionHints(cmd *cobra.Command, plan wodby1.Plan) {
	if plan.Source.Kind != "app" && plan.Source.Kind != "server" {
		return
	}
	blocked := map[string]bool{}
	for _, item := range plan.Review {
		if item.Severity != wodby1.SeverityBlocking || item.Instance == "" {
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

func printArtifactNotice(cmd *cobra.Command, planPath, statePath string) {
	if planOutputJSON(cmd) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary migration plan: %s\n", planPath)
		fmt.Fprintf(cmd.ErrOrStderr(), "Temporary resume state: %s\n", statePath)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Temporary resume-state file: %s\n", statePath)
	fmt.Fprintln(cmd.OutOrStdout(), "Keep these files until --verify succeeds. Starting migration...")
}

func printServerArtifactNotice(cmd *cobra.Command, planPath, statePath string) {
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
	cmd *cobra.Command,
	action string,
	plan wodby1.Plan,
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

func publicMigrationStatus(apply bool) string {
	if apply {
		return "applied; awaiting DNS verification"
	}
	return "verified"
}

func printServerMigrationResult(
	cmd *cobra.Command,
	action string,
	plan wodby1.Plan,
	planPath string,
	apps []serverAppMigrationOutput,
) error {
	output := serverMigrationOutput{Action: action, Plan: plan, PlanFile: planPath, Apps: apps}
	if planOutputJSON(cmd) {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return errors.WithStack(encoder.Encode(output))
	}
	failed := 0
	for _, app := range apps {
		if app.Error != "" {
			failed++
		}
	}
	color := cliColorGreen
	message := fmt.Sprintf("Server migration %s completed for %d app(s).", action, len(apps))
	if failed != 0 {
		color = cliColorOrange
		message = fmt.Sprintf("Server migration %s processed %d app(s): %d succeeded, %d failed.", action, len(apps), len(apps)-failed, failed)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliColor(cmd.OutOrStdout(), color, message))
	for _, app := range apps {
		fmt.Fprintf(cmd.OutOrStdout(), "App %s: status=%s\n", app.Name, app.Status)
		if viper.GetBool("verbose") {
			fmt.Fprintf(cmd.OutOrStdout(), "  Resume-state file: %s\n", app.StateFile)
		}
		if app.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  Error: %s\n", app.Error)
		}
	}
	printImportStatuses(cmd, plan, "completed")
	if action == "apply" {
		fmt.Fprintln(cmd.OutOrStdout(), "Test every app using its Wodby 2 technical route before changing DNS.")
		fmt.Fprintln(cmd.OutOrStdout(), "After DNS points to Wodby 2, rerun the same command with --verify.")
		if viper.GetBool("verbose") {
			fmt.Fprintf(cmd.OutOrStdout(), "Temporary plan file: %s\n", planPath)
		}
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Target resources, DNS, routes, certificates, and data imports match the applied migrations.")
	}
	return nil
}

func printImportStatuses(cmd *cobra.Command, plan wodby1.Plan, status string) {
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
