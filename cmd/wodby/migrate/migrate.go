package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

const defaultSourceBaseURL = "https://api.wodby.com"

type options struct {
	sourceBaseURL       string
	sourceToken         string
	includeSecrets      bool
	createSourceBackup  bool
	targetOrg           string
	targetProject       string
	targetCluster       string
	targetEnvMap        []string
	createMissingEnvs   bool
	stackMap            []string
	serviceMap          []string
	dryRun              bool
	execute             bool
	planFile            string
	stateFile           string
	resume              bool
	parallel            int
	continueOnError     bool
	allowMissingSecrets bool
	yes                 bool
	acceptReview        bool
	output              string
	assumeEnvoyGateway  bool
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migration tools",
	}
	cmd.AddCommand(newWodby1Command())
	return cmd
}

func newWodby1Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wodby1",
		Short: "Migrate Wodby 1 apps to Wodby 2",
	}
	cmd.AddCommand(newWodby1AppCommand(), newWodby1ServerCommand())
	return cmd
}

func newWodby1AppCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "app SOURCE_APP_UUID",
		Short: "Plan a Wodby 1 app migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1(cmd, "app", args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	return cmd
}

func newWodby1ServerCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "server SOURCE_SERVER_UUID",
		Short: "Plan a Wodby 1 server-to-cluster migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWodby1(cmd, "server", args[0], opts)
		},
	}
	bindFlags(cmd, opts)
	return cmd
}

func defaultOptions() *options {
	return &options{
		sourceBaseURL: defaultSourceBaseURL,
		parallel:      1,
		output:        "text",
	}
}

func bindFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.sourceBaseURL, "source-base-url", defaultSourceBaseURL, "Wodby 1 API base URL")
	cmd.Flags().StringVar(&opts.sourceToken, "source-token", "", "Wodby 1 API token")
	cmd.Flags().BoolVar(&opts.includeSecrets, "include-secrets", false, "Ask the source API to include protected secret values")
	cmd.Flags().BoolVar(&opts.createSourceBackup, "create-source-backup", false, "Create missing source backups before migration execution")
	cmd.Flags().StringVar(&opts.targetOrg, "target-org", "", "Target Wodby 2 org name or ID")
	cmd.Flags().StringVar(&opts.targetProject, "target-project", "", "Target Wodby 2 project name or ID")
	cmd.Flags().StringVar(&opts.targetCluster, "target-cluster", "", "Target Wodby 2 cluster name or ID")
	cmd.Flags().StringArrayVar(&opts.targetEnvMap, "target-env-map", nil, "Source-to-target env mapping, e.g. prod=production")
	cmd.Flags().BoolVar(&opts.createMissingEnvs, "create-missing-envs", false, "Create missing target envs during execution")
	cmd.Flags().StringArrayVar(&opts.stackMap, "stack-map", nil, "Managed stack mapping override, e.g. drupal9=drupal10")
	cmd.Flags().StringArrayVar(&opts.serviceMap, "service-map", nil, "Service mapping override, e.g. redis=valkey")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Plan only; this is also the default when --execute is omitted")
	cmd.Flags().BoolVar(&opts.execute, "execute", false, "Execute the reviewed migration plan")
	cmd.Flags().StringVar(&opts.planFile, "plan-file", "", "Write the normalized migration plan to this JSON file")
	cmd.Flags().StringVar(&opts.stateFile, "state-file", "", "Path to resumable migration state file")
	cmd.Flags().BoolVar(&opts.resume, "resume", false, "Resume from the state file")
	cmd.Flags().IntVar(&opts.parallel, "parallel", 1, "Parallel app migrations for server migrations")
	cmd.Flags().BoolVar(&opts.continueOnError, "continue-on-error", false, "Continue server migration after an app failure")
	cmd.Flags().BoolVar(&opts.allowMissingSecrets, "allow-missing-secrets", false, "Allow redacted secrets as manual follow-up items")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Confirm clean managed-stack execution non-interactively")
	cmd.Flags().BoolVar(&opts.acceptReview, "accept-review", false, "Accept non-blocking review items non-interactively")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&opts.assumeEnvoyGateway, "assume-envoy-gateway", false, "Assume the target cluster supports Envoy Gateway redirect routes during planning")
}

func runWodby1(cmd *cobra.Command, kind string, id string, opts *options) error {
	if err := validateOptions(cmd, opts); err != nil {
		return err
	}

	client, err := wodby1.NewSourceClient(opts.sourceBaseURL, opts.sourceToken)
	if err != nil {
		return err
	}

	var export wodby1.Export
	switch kind {
	case "app":
		export, err = client.ExportApp(cmd.Context(), id, opts.includeSecrets)
	case "server":
		export, err = client.ExportServer(cmd.Context(), id, opts.includeSecrets)
	default:
		return errors.Errorf("unsupported migration kind %q", kind)
	}
	if err != nil {
		return err
	}

	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}

	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:          kind,
		SourceID:            id,
		TargetOrg:           opts.targetOrg,
		TargetProject:       opts.targetProject,
		TargetCluster:       opts.targetCluster,
		TargetEnvMap:        envMap,
		AllowMissingSecrets: opts.allowMissingSecrets,
		AssumeEnvoyGateway:  opts.assumeEnvoyGateway,
	})
	if err != nil {
		return err
	}

	if opts.planFile != "" {
		if err := writePlanFile(opts.planFile, plan); err != nil {
			return err
		}
	}

	if opts.output == "json" {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			return errors.WithStack(err)
		}
	} else {
		wodby1.PrintReview(cmd.OutOrStdout(), plan)
	}

	if opts.execute {
		return errors.New("migration execution is not implemented yet; rerun without --execute to use the dry-run planner")
	}
	return nil
}

func validateOptions(cmd *cobra.Command, opts *options) error {
	if opts.execute && cmd.Flags().Changed("dry-run") && opts.dryRun {
		return errors.New("--dry-run and --execute are mutually exclusive")
	}
	if opts.sourceBaseURL == "" {
		return errors.New("--source-base-url is required")
	}
	if _, err := url.ParseRequestURI(opts.sourceBaseURL); err != nil {
		return errors.Wrap(err, "invalid --source-base-url")
	}
	if strings.TrimSpace(opts.sourceToken) == "" {
		return errors.New("--source-token is required")
	}
	if opts.output != "text" && opts.output != "json" {
		return errors.Errorf("unsupported --output %q", opts.output)
	}
	if opts.parallel < 1 {
		return errors.New("--parallel must be greater than zero")
	}
	if opts.execute {
		if opts.targetOrg == "" || opts.targetProject == "" || opts.targetCluster == "" {
			return errors.New("--target-org, --target-project, and --target-cluster are required with --execute")
		}
		if opts.yes && opts.acceptReview {
			return errors.New("use either --yes or --accept-review, not both")
		}
	}
	if opts.resume && opts.stateFile == "" {
		return errors.New("--resume requires --state-file")
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
			key = strings.TrimSpace(key)
			target = strings.TrimSpace(target)
			if key == "" || target == "" {
				return nil, fmt.Errorf("%s value %q must be in source=target format", flag, part)
			}
			result[key] = target
		}
	}
	return result, nil
}

func writePlanFile(path string, plan wodby1.Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}
	data = append(data, '\n')
	return errors.WithStack(os.WriteFile(path, data, 0600))
}
