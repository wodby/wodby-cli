package init

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/ci"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
	"github.com/wodby/wodby-cli/pkg/version"
)

type options struct {
	id             int
	context        string
	fixPermissions bool
	buildNumber    int
	buildID        string
	provider       string
}

var opts options

var Cmd = &cobra.Command{
	Use:   "init [OPTIONS] WODBY_BUILD_ID|WODBY_GIT_REPO_ID",
	Short: "Initialize config for CI process",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if viper.GetString("api_key") == "" && viper.GetString("access_token") == "" {
			return errors.New("either api-key or access-token must be specified")
		}
		if viper.GetString("api_endpoint") == "" {
			return errors.New("api-endpoint flag is required")
		}

		var err error
		opts.id, err = strconv.Atoi(args[0])
		if err != nil {
			return errors.WithStack(err)
		}

		if opts.context != "" {
			opts.context, err = filepath.Abs(opts.context)
			if err != nil {
				return errors.WithStack(err)
			}
		} else {
			opts.context, err = os.Getwd()
			if err != nil {
				return errors.WithStack(err)
			}
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		apiConfig := types.APIConfig{
			Key:         viper.GetString("api_key"),
			Endpoint:    viper.GetString("api_endpoint"),
			AccessToken: viper.GetString("access_token"),
		}
		client := api.NewClient(apiConfig)

		logger := log.WithField("stage", "init")
		log.SetOutput(os.Stdout)
		if viper.GetBool("verbose") {
			log.SetLevel(log.DebugLevel)
		}

		logger.Info("Checking CLI version...")
		if version.VERSION == "dev" {
			logger.Warn("You're using a dev version of CLI, some things may be unstable. Skipping version check")
		} else {
			//ver, err := client.GetLatestVersion()
			//if err != nil {
			//	return err
			//}
			//
			//v1, err := semver.Make(version.VERSION)
			//v2, err := semver.Make(ver)
			//if v1.Compare(v2) == -1 {
			//	return fmt.Errorf("current version of CLI (%s) is outdated, minimum required is %s, please upgrade", v1.String(), v2.String())
			//}
		}

		ctx := context.Background()

		var appBuild types.AppBuild
		var err error

		if os.Getenv("WODBY_CI") == "" {
			logger.Infof("Creating new app build from CI for git repo %d...", opts.id)
			input, err := ci.CollectBuildInfo()
			if err != nil {
				return errors.WithStack(err)
			}
			input.GitRepoID = opts.id
			if input.BuildID == "" {
				if opts.buildID == "" {
					return errors.New("build id must be specified")
				}
				input.BuildID = opts.buildID
			}
			if input.BuildNum == 0 {
				if opts.buildNumber == 0 {
					return errors.New("build number must be specified")
				}
				input.BuildNum = opts.buildNumber
			}
			if input.Provider == "" {
				if opts.provider == "" {
					return errors.New("provider must be specified")
				}
				input.Provider = opts.provider
			}

			appBuild, err = client.NewCIBuild(context.Background(), input)
			if err != nil {
				return errors.WithStack(err)
			}
		} else {
			logger.Infof("Requesting info for app build %d...", opts.id)
			appBuild, err = client.GetAppBuild(ctx, opts.id)
			if err != nil {
				return errors.WithStack(err)
			}
		}

		logger.Infof("Requesting registry credentials for app build %d...", opts.id)
		credentials, err := client.GetDockerRegistryCredentials(context.Background(), appBuild.ID)
		if err != nil {
			return errors.WithStack(err)
		}
		dockerClient := docker.NewClient()
		logger.Info("Logging in the docker registry...")
		err = dockerClient.Login(appBuild.Config.RegistryHost, credentials.Username, credentials.Password)
		if err != nil {
			return errors.WithStack(err)
		}

		config := types.Config{
			API:      apiConfig,
			ID:       opts.id,
			Context:  opts.context,
			AppBuild: appBuild,
		}
		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = os.WriteFile(path.Join(viper.GetString("ci_config_path")), content, 0600)
		if err != nil {
			return errors.WithStack(err)
		}

		for _, appServiceBuildConfig := range appBuild.Config.Services {
			if appServiceBuildConfig.Main {
				// We will fix permissions either when it was instructed or when it's a managed service.
				if os.Getenv("WODBY_CI") != "" && (opts.fixPermissions || appServiceBuildConfig.Managed) {
					if opts.fixPermissions {
						logger.Info("Fixing codebase permissions...")
					} else {
						logger.Infof("Fixing permissions for managed service %s", appServiceBuildConfig.Title)
					}
					defaultUser, err := dockerClient.GetImageDefaultUser(appServiceBuildConfig.Image)
					if err != nil {
						return errors.WithStack(err)
					}
					workingDir, err := dockerClient.GetImageWorkingDir(appServiceBuildConfig.Image)
					if err != nil {
						return errors.WithStack(err)
					}

					if defaultUser != "root" {
						runConfig := docker.RunConfig{
							Image: appServiceBuildConfig.Image,
							User:  "root",
						}
						runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", opts.context, workingDir))
						args := []string{"chown", "-R", fmt.Sprintf("%s:%s", defaultUser, defaultUser), "."}
						err := dockerClient.Run(args, runConfig)
						if err != nil {
							return errors.WithStack(err)
						}
					} else {
						logger.Debug("Default user of the default service is root, skipping permissions fix")
					}
				}
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&opts.context, "context", "c", "", "Build context (default: current directory)")
	Cmd.Flags().BoolVar(&opts.fixPermissions, "fix-permissions", false, "Fix codebase permissions. Performed automatically for known CI environments. WARNING: make sure you run wodby ci init from the project directory")
	Cmd.Flags().IntVarP(&opts.buildNumber, "build-num", "n", 0, "Custom build number (used if can't identify automatically)")
	Cmd.Flags().StringVarP(&opts.buildID, "build-id", "bid", "", "Custom build id (used if can't identify automatically)")
	Cmd.Flags().StringVar(&opts.provider, "provider", "p", "Custom build provider name (used if can't identify automatically)")
}
