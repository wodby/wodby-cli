package migrate

import (
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
	"github.com/wodby/wodby-cli/pkg/types"
)

const (
	defaultSourceBaseURL = wodby1.DefaultSourceBaseURL
	sourceTokenEnv       = wodby1.SourceTokenEnv
)

// options are the migration flags; the migration itself runs in the library.
type options = wodby1.RunOptions

func defaultOptions() *options {
	return wodby1.DefaultRunOptions()
}

func runWodby1Single(cmd *cobra.Command, sourceKind string, sourceID string, opts *options) error {
	env, err := commandRunEnv(cmd, opts)
	if err != nil {
		return err
	}
	return wodby1.RunSingle(env, sourceKind, sourceID, opts)
}

func runWodby1Server(cmd *cobra.Command, sourceID string, opts *options) error {
	env, err := commandRunEnv(cmd, opts)
	if err != nil {
		return err
	}
	return wodby1.RunServer(env, sourceID, opts)
}

// commandRunEnv runs a migration on the terminal with the Wodby 2 credentials
// of the CLI configuration, keeping the plan and state in files.
func commandRunEnv(cmd *cobra.Command, opts *options) (*wodby1.RunEnv, error) {
	opts.SourceToken = firstNonBlank(opts.SourceToken, os.Getenv(sourceTokenEnv))
	if err := wodby1.ValidateRunOptions(opts); err != nil {
		return nil, err
	}
	if strings.TrimSpace(viper.GetString("api_base_url")) == "" {
		return nil, errors.New("--api-base-url is required")
	}
	if strings.TrimSpace(viper.GetString("api_key")) == "" {
		return nil, errors.New("--api-key is required; Wodby 2 access tokens cannot authorize customer migrations")
	}
	target, err := wodby1.NewTargetClient(types.APIConfig{
		Endpoint: strings.TrimSpace(viper.GetString("api_base_url")),
		Key:      strings.TrimSpace(viper.GetString("api_key")),
	})
	if err != nil {
		return nil, err
	}
	return &wodby1.RunEnv{
		Ctx:     cmd.Context(),
		In:      cmd.InOrStdin(),
		Out:     cmd.OutOrStdout(),
		Err:     cmd.ErrOrStderr(),
		JSON:    planOutputJSON(cmd),
		Verbose: viper.GetBool("verbose"),
		Target:  target,
		RestartSuggestion: func(sourceKind, sourceID string) string {
			return restartCommandSuggestion(cmd, sourceKind, sourceID)
		},
		SilenceUsage: func() { cmd.Root().SilenceUsage = true },
		SilenceErrors: func() {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		},
	}, nil
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
Add --workspace to create an instance that uses direct Git deployment as a
development workspace: its code is checked out from the linked Git repository
instead of being built by CI. The preview reports why an instance cannot be one.

The migration exports only the selected
instance and its parent app metadata, then creates a new Wodby 2 app containing
a matching app environment. Pass --target-app to add it to an existing Wodby 2 app instead;
the CLI infers the app's shared stack when unambiguous and never adopts or
overwrites an existing app environment. By default it imports the newest successful backup file for each
required component shown in the preview. Use --source-backup BACKUP_UUID to pin
all components to a different complete snapshot, or
--skip-data to omit data. Changes made after the selected backup completed are
not migrated. Apply is resumable and stores its plan and state in the system
temporary directory, or next to --state-file when it is set. When state exists, the same --apply command preserves the saved plan
and continues completed work. --apply --restart replaces the saved plan. If the
migration already changed Wodby 2, the CLI first shows and confirms removal of
only the resources recorded as created by this migration; pre-existing target
apps and reused resources are preserved. Restart is refused after successful
verification or while any target operation remains ambiguous. --rollback
deletes the Wodby 2 resources this migration created and discards its state; it
refuses once --verify has succeeded, because DNS then points at Wodby 2.
Migrated cron schedules stay disabled until --verify confirms that DNS points
to Wodby 2, so scheduled jobs do not run on both platforms.`,
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
	cmd.Flags().StringVar(&opts.TargetApp, "target-app", "", "Existing Wodby 2 app ID or exact name to receive the migrated app environment")
	cmd.Flags().BoolVar(&opts.Workspace, "workspace", false, "Create this direct-Git instance as a Wodby 2 development workspace instead of an app environment built by CI")
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
Use repeatable --workspace-instance APP/INSTANCE values to create instances that
use direct Git deployment as development workspaces: their code is checked out
from the linked Git repository instead of being built by CI.
By default, the newest successful file for each required backup component is selected.
Use repeatable --source-backup APP/INSTANCE=BACKUP_UUID values to override
individual snapshots, or --skip-data to omit data. Changes after each selected
backup completed are not migrated. Test the
migrated apps before changing DNS, then use --verify. When per-app state exists,
--apply preserves the aggregate saved plan and continues completed work;
--apply --restart replaces the saved plan after showing and confirming removal
of only resources recorded as created by these migrations. Pre-existing target
apps and reused resources are preserved. Restart is refused after successful
verification or while any target operation remains ambiguous. --rollback
deletes the Wodby 2 resources this migration created and discards its state; it
refuses once --verify has succeeded, because DNS then points at Wodby 2.
Migrated cron schedules stay disabled until --verify confirms that DNS points
to Wodby 2, so scheduled jobs do not run on both platforms.`,
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
		&opts.ExcludeApps,
		"exclude-app",
		nil,
		"Exclude an app by exact UUID or name (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.ExcludeInstances,
		"exclude-instance",
		nil,
		"Exclude an instance by UUID or APP/INSTANCE (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.WorkspaceInstances,
		"workspace-instance",
		nil,
		"Create a direct-Git instance, by UUID or APP/INSTANCE, as a Wodby 2 development workspace instead of an app environment built by CI (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.TargetRepositoryMap,
		"target-repository-map",
		nil,
		"Per-app Git target (APP=GIT_INTEGRATION_ID[:REPOSITORY_NAME[:SERVICE]])",
	)
	cmd.Flags().Lookup("state-file").Usage = "Base path for secure per-app resume-state files; the plan is kept next to it (defaults to the system temporary directory)"
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
Use repeatable --workspace-instance INSTANCE values to create instances that use
direct Git deployment as development workspaces: their code is checked out from
the linked Git repository instead of being built by CI.
Changes after each selected backup completed are not migrated. If a target mutation is ambiguous, inspect Wodby 2 and pass
--retry-ambiguous only with the exact operation ID printed by the command. When
state exists, --apply preserves the saved plan and continues completed work;
--apply --restart replaces the saved plan. If the migration already changed
Wodby 2, the CLI first shows and confirms removal of only resources recorded as
created by this migration; pre-existing target apps and reused resources are
preserved. Restart is refused after successful verification or while any target
operation remains ambiguous. --rollback deletes the Wodby 2 resources this
migration created and discards its state; it refuses once --verify has
succeeded, because DNS then points at Wodby 2.
Migrated cron schedules stay disabled until --verify confirms that DNS points
to Wodby 2, so scheduled jobs do not run on both platforms.`,
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
		&opts.ExcludeInstances,
		"exclude-instance",
		nil,
		"Exclude an instance by exact UUID or name (repeatable)",
	)
	cmd.Flags().StringArrayVar(
		&opts.WorkspaceInstances,
		"workspace-instance",
		nil,
		"Create a direct-Git instance, by exact UUID or name, as a Wodby 2 development workspace instead of an app environment built by CI (repeatable)",
	)
	return cmd
}

func bindFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.SourceBaseURL, "source-base-url", defaultSourceBaseURL, "Wodby 1 API base URL")
	cmd.Flags().StringVar(&opts.SourceToken, "source-token", "", "Wodby 1 API token (defaults to "+sourceTokenEnv+")")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "Create the target and import data using the displayed plan")
	cmd.Flags().BoolVar(&opts.Verify, "verify", false, "Verify the applied migration after testing and DNS cutover")
	cmd.Flags().BoolVar(&opts.Restart, "restart", false, "Replace the saved plan, safely removing migration-created target resources first when necessary (requires --apply)")
	cmd.Flags().BoolVar(&opts.Rollback, "rollback", false, "Delete the Wodby 2 resources this migration created and discard its resume state")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Approve a new migration plan without an interactive prompt")

	cmd.Flags().StringVar(&opts.TargetProject, "target-project", "", "Wodby 2 project ID or exact name (defaults to a project-owned cluster's owner; otherwise organization-owned)")
	cmd.Flags().StringVar(&opts.TargetCluster, "target-cluster", "", "Wodby 2 cluster ID or exact name")
	cmd.Flags().StringArrayVar(&opts.TargetEnvMap, "target-env-map", nil, "Map a Wodby 1 instance type to a Wodby 2 environment type (SOURCE=prod|staging|test|dev|feature)")
	cmd.Flags().IntVar(&opts.TargetStackID, "target-stack-id", 0, "Existing Wodby 2 stack ID override (managed Drupal/WordPress apps get a new catalog stack by default)")
	cmd.Flags().StringArrayVar(&opts.TargetStackMap, "target-stack-map", nil, "Source-to-target stack ID mapping ([APP/][INSTANCE/]SOURCE=TARGET_STACK_ID)")
	cmd.Flags().StringArrayVar(&opts.TargetServiceMap, "target-service-map", nil, "Source-to-target service mapping ([APP/][INSTANCE/]SOURCE=TARGET)")
	cmd.Flags().StringArrayVar(&opts.TargetVersionMap, "target-version-map", nil, "Source-service version override ([APP/][INSTANCE/]SOURCE_SERVICE=TARGET_VERSION)")
	cmd.Flags().StringArrayVar(&opts.TargetImportMap, "target-import-map", nil, "Backup-to-import mapping ([APP/][INSTANCE/]COMPONENT=SERVICE:IMPORT)")

	cmd.Flags().IntVar(&opts.TargetCIIntegrationID, "target-ci-integration-id", 0, "Wodby 2 CI integration ID override (otherwise Wodby CI with a linked repository, or automatic Custom CI fallback)")
	cmd.Flags().IntVar(&opts.TargetGitIntegrationID, "target-git-integration-id", 0, "Optional Wodby 2 Git integration ID used to find and link the source repository")
	cmd.Flags().StringVar(&opts.TargetRepositoryName, "target-repository-name", "", "Exact repository name in the selected Wodby 2 Git integration (defaults to the name derived from the Wodby 1 repository URL)")
	cmd.Flags().StringVar(&opts.TargetCodeService, "target-code-service", "", "Target connect-build service name when the stack has more than one")
	cmd.Flags().StringVar(&opts.TargetGitRef, "target-git-ref", "", "Git branch, tag, or commit to build (defaults to the source ref)")
	cmd.Flags().StringVar(&opts.TargetGitRefType, "target-git-ref-type", "", "Git ref type: branch, tag, or commit")

	cmd.Flags().StringVar(&opts.WorkspaceStorageClass, "workspace-storage-class", "", "Storage class for the code and home volumes of development workspaces (defaults to the cluster's default storage class)")
	cmd.Flags().StringVar(&opts.WorkspaceStorageService, "workspace-storage-service", "", "Enabled storage service of the target stack that holds the code and home volumes of development workspaces instead of a storage class")
	cmd.Flags().BoolVar(&opts.SkipCode, "skip-code", false, "Intentionally omit repository/build-source migration")
	cmd.Flags().BoolVar(&opts.SkipData, "skip-data", false, "Intentionally omit database and files imports")
	cmd.Flags().StringArrayVar(&opts.SourceBackups, "source-backup", nil, "Select a successful Wodby 1 backup (BACKUP_UUID for an instance, INSTANCE=BACKUP_UUID for an app, APP/INSTANCE=BACKUP_UUID for a server; repeatable)")
	cmd.Flags().BoolVar(
		&opts.AllowUnsupportedDrupal,
		"allow-unsupported-drupal",
		false,
		"Allow Drupal 8/9 when the nginx preset does not identify Drupal 10 or newer and you have independently confirmed the application was upgraded",
	)
	cmd.Flags().BoolVar(
		&opts.AddMissingServices,
		"add-missing-services",
		true,
		"Add mapped services missing from the target stack when an exact Wodby 2 service is available",
	)

	cmd.Flags().StringVar(&opts.StateFile, "state-file", "", "Secure resume-state path; the plan is kept next to it (defaults to the system temporary directory)")
	cmd.Flags().DurationVar(&opts.PollInterval, "poll-interval", 2*time.Second, "Target operation polling interval")
	cmd.Flags().DurationVar(&opts.WaitTimeout, "wait-timeout", 30*time.Minute, "Timeout for each target operation")
	cmd.Flags().StringVar(&opts.RetryAmbiguous, "retry-ambiguous", "", "Retry exactly one inspected ambiguous operation ID")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "text", "Output format: text or json")
}

// restartCommandSuggestion reconstructs the current invocation without secret
// values. It includes every explicitly supplied migration option, replacing
// the run mode with --apply --restart so the suggested command preserves the
// reviewed target and mappings.
func restartCommandSuggestion(cmd *cobra.Command, sourceKind string, sourceID string) string {
	args := []string{"wodby", "migrate", "wodby1", sourceKind, sourceID}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "apply", "verify", "restart", "rollback", "source-token":
			return
		// Visit also walks the root command's flags, which carry the
		// Wodby 2 credentials.
		case "api-key", "access-token":
			return
		}
		name := "--" + flag.Name
		if values, ok := flag.Value.(pflag.SliceValue); ok {
			for _, value := range values.GetSlice() {
				args = append(args, name, shellCommandArgument(value))
			}
			return
		}
		if flag.Value.Type() == "bool" && flag.Value.String() == "true" {
			args = append(args, name)
			return
		}
		args = append(args, name, shellCommandArgument(flag.Value.String()))
	})
	args = append(args, "--apply", "--restart")
	return strings.Join(args, " ")
}

func shellCommandArgument(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("._:/=@,+-", r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runWodby1App(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	return runWodby1Single(cmd, "app", sourceID, opts)
}

func runWodby1Instance(cmd *cobra.Command, sourceID string, opts *options) (runErr error) {
	return runWodby1Single(cmd, "instance", sourceID, opts)
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
