package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	sourceBaseURL       string
	sourceToken         string
	includeSecrets      bool
	targetOrg           string
	targetProject       string
	targetCluster       string
	targetEnvMap        []string
	planFile            string
	allowMissingSecrets bool
	output              string
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "migrate",
		Short:  "Migration tools",
		Hidden: true,
	}
	cmd.AddCommand(newWodby1Command())
	return cmd
}

func newWodby1Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wodby1",
		Short: "Inspect Wodby 1 exports and build read-only migration plans",
	}
	cmd.AddCommand(newWodby1AppCommand(), newWodby1ServerCommand())
	return cmd
}

func newWodby1AppCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "app SOURCE_APP_UUID",
		Short: "Build a read-only Wodby 1 app migration plan",
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
		Short: "Build a read-only Wodby 1 server migration plan",
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
		output:        "text",
	}
}

func bindFlags(cmd *cobra.Command, opts *options) {
	cmd.Flags().StringVar(&opts.sourceBaseURL, "source-base-url", defaultSourceBaseURL, "Wodby 1 API base URL")
	cmd.Flags().StringVar(&opts.sourceToken, "source-token", "", "Wodby 1 API token (defaults to "+sourceTokenEnv+")")
	cmd.Flags().BoolVar(&opts.includeSecrets, "include-secrets", false, "Ask the source API to include protected secret values")
	cmd.Flags().StringVar(&opts.targetOrg, "target-org", "", "Record the intended Wodby 2 org selector in the plan")
	cmd.Flags().StringVar(&opts.targetProject, "target-project", "", "Record the intended Wodby 2 project selector in the plan")
	cmd.Flags().StringVar(&opts.targetCluster, "target-cluster", "", "Record the intended Wodby 2 cluster selector in the plan")
	cmd.Flags().StringArrayVar(&opts.targetEnvMap, "target-env-map", nil, "Source-to-target env mapping, e.g. prod=production")
	cmd.Flags().StringVar(&opts.planFile, "plan-file", "", "Write the read-only migration plan to this JSON file")
	cmd.Flags().BoolVar(&opts.allowMissingSecrets, "allow-missing-secrets", false, "Allow redacted secrets as manual follow-up items")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "Output format: text or json")
}

func runWodby1(cmd *cobra.Command, kind string, id string, opts *options) error {
	if strings.TrimSpace(opts.sourceToken) == "" {
		opts.sourceToken = strings.TrimSpace(os.Getenv(sourceTokenEnv))
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	envMap, err := parseMapping(opts.targetEnvMap, "--target-env-map")
	if err != nil {
		return err
	}
	client, err := wodby1.NewSourceClient(opts.sourceBaseURL, opts.sourceToken)
	if err != nil {
		return err
	}

	targetAdminVerified := false
	var targetClient *wodby1.TargetClient
	var targetScope *wodby1.TargetScopeDiscovery
	if hasTarget(opts) {
		targetClient, err = wodby1.NewTargetClient(types.APIConfig{
			Endpoint:    strings.TrimSpace(viper.GetString("api_base_url")),
			Key:         strings.TrimSpace(viper.GetString("api_key")),
			AccessToken: strings.TrimSpace(viper.GetString("access_token")),
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
		targetScope = &scope
		targetAdminVerified = scope.User.IsAdmin
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

	targetEnvs := map[string]wodby1.TargetEnv{}
	if targetClient != nil && targetScope != nil {
		selectors, err := wodby1.TargetEnvironmentSelectors(export, envMap)
		if err != nil {
			return err
		}
		resolved, err := targetClient.ResolveTargetEnvs(cmd.Context(), targetScope.Org.ID, selectors)
		if err != nil {
			return err
		}
		for _, item := range resolved {
			targetEnvs[item.Selector] = item.Env
		}
	}

	plan, err := wodby1.BuildPlan(export, wodby1.PlanOptions{
		SourceKind:          kind,
		SourceID:            id,
		TargetOrg:           opts.targetOrg,
		TargetProject:       opts.targetProject,
		TargetCluster:       opts.targetCluster,
		TargetEnvMap:        envMap,
		AllowMissingSecrets: opts.allowMissingSecrets,
		TargetAdminVerified: targetAdminVerified,
		TargetScope:         targetScope,
		TargetEnvs:          targetEnvs,
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

	return nil
}

func validateOptions(opts *options) error {
	if opts.sourceBaseURL == "" {
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
	if hasAnyTarget(opts) && !hasTarget(opts) {
		return errors.New("--target-org, --target-project, and --target-cluster must be specified together")
	}
	if hasTarget(opts) {
		if strings.TrimSpace(viper.GetString("api_base_url")) == "" {
			return errors.New("--api-base-url is required when a target is specified")
		}
		if strings.TrimSpace(viper.GetString("api_key")) == "" &&
			strings.TrimSpace(viper.GetString("access_token")) == "" {
			return errors.New("--api-key or --access-token is required when a target is specified")
		}
	}
	return nil
}

func hasAnyTarget(opts *options) bool {
	return strings.TrimSpace(opts.targetOrg) != "" ||
		strings.TrimSpace(opts.targetProject) != "" ||
		strings.TrimSpace(opts.targetCluster) != ""
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
			key = strings.TrimSpace(key)
			target = strings.TrimSpace(target)
			if key == "" || target == "" {
				return nil, fmt.Errorf("%s value %q must be in source=target format", flag, part)
			}
			key = strings.ToLower(key)
			if existing, exists := result[key]; exists && existing != target {
				return nil, fmt.Errorf("%s contains conflicting mappings for source %q", flag, key)
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
