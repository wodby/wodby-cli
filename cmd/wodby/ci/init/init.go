package init

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
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
	dind           bool
	fixPermissions bool
	buildNumber    int
	buildID        string
	provider       string
}

var opts options

var Cmd = &cobra.Command{
	Use:   "init [OPTIONS] WODBY_APP_SERVICE_ID|WODBY_BUILD_ID",
	Short: "Initialize config for CI process",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if viper.GetString("api_key") == "" && viper.GetString("access_token") == "" {
			return errors.New("either api-key or access-token must be specified")
		}
		if viper.GetString("api_base_url") == "" {
			return errors.New("api-base-url flag is required")
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
			Endpoint:    viper.GetString("api_base_url"),
			AccessToken: viper.GetString("access_token"),
		}
		client, err := api.NewClient(apiConfig)
		if err != nil {
			return errors.WithStack(err)
		}

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

		if os.Getenv("WODBY_CI") == "" {
			logger.Infof("Creating new app build from CI for app service %d...", opts.id)
			input, err := ci.CollectBuildInfo()
			if err != nil {
				return errors.WithStack(err)
			}
			input.AppServiceID = types.ToID(opts.id)
			if err = applyCIBuildInputFlags(&input, opts); err != nil {
				return err
			}
			postDeployment, err := readPostDeployment(opts.context)
			if err != nil {
				return errors.WithStack(err)
			}
			if postDeployment != "" {
				input.PostDeployment = &postDeployment
			}

			appBuild, err = client.NewCIBuild(ctx, input)
			if err != nil {
				return errors.WithStack(err)
			}
		} else {
			logger.Infof("Requesting info for app build %d...", opts.id)
			appBuild, err = client.GetAppBuild(ctx, types.ToID(opts.id))
			if err != nil {
				return errors.WithStack(err)
			}
		}

		logger.Infof("Requesting config for app build %s...", appBuild.ID)
		appBuildConfig, err := client.GetAppBuildConfig(ctx, appBuild.ID)
		if err != nil {
			return errors.WithStack(err)
		}
		appBuild.Config = &appBuildConfig

		logger.Infof("Requesting registry credentials for app build %s...", appBuild.ID)
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

		mainService, err := findMainServiceBuildConfig(appBuild.Config.Services)
		if err != nil {
			return errors.WithStack(err)
		}
		workingDir, err := dockerClient.GetImageWorkingDir(mainService.Image)
		if err != nil {
			return errors.WithStack(err)
		}

		config := types.Config{
			API:        apiConfig,
			ID:         strconv.Itoa(opts.id),
			WorkingDir: workingDir,
			Context:    opts.context,
			AppBuild:   appBuild,
		}

		if opts.dind {
			logger.Info("Using docker in docker build schema. Creating data container...")
			config.DataContainer = uuid.NewString()

			output, err := exec.Command("docker", "pull", "alpine").CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}

			output, err = exec.Command(
				"docker",
				"create",
				fmt.Sprintf("--volume=%s", config.WorkingDir),
				fmt.Sprintf("--name=%s", config.DataContainer),
				"alpine",
				"/bin/true",
			).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}

			output, err = exec.Command(
				"docker",
				"cp",
				fmt.Sprintf("%s/.", config.Context),
				fmt.Sprintf("%s:%s", config.DataContainer, config.WorkingDir),
			).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = os.WriteFile(path.Join(viper.GetString("ci_config_path")), content, 0600)
		if err != nil {
			return errors.WithStack(err)
		}

		shouldFixPermissions, permissionFixReason := permissionFixDecision(
			opts.fixPermissions,
		)

		if shouldFixPermissions {
			logger.Infof("Fixing codebase permissions: %s", permissionFixReason)

			defaultUser, err := dockerClient.GetImageDefaultUser(mainService.Image)
			if err != nil {
				return errors.WithStack(err)
			}

			if defaultUser != "root" {
				runConfig := docker.RunConfig{
					Image: mainService.Image,
					User:  "root",
				}
				if config.DataContainer != "" {
					runConfig.VolumesFrom = []string{config.DataContainer}
				} else {
					runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", opts.context, config.WorkingDir))
				}

				args := []string{"chown", "-R", docker.ChownSpec(defaultUser), "."}
				err := dockerClient.Run(args, runConfig)
				if err != nil {
					return errors.WithStack(err)
				}
			} else {
				logger.Info("Skipping codebase permissions fix: main service image default user is root")
			}
		} else {
			logger.Infof("Skipping codebase permissions fix: %s", permissionFixReason)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&opts.context, "context", "c", "", "Build context (default: current directory)")
	Cmd.Flags().BoolVar(&opts.dind, "dind", false, "Use data container for sharing files between commands")
	Cmd.Flags().BoolVar(&opts.fixPermissions, "fix-permissions", false, "Fix codebase permissions explicitly. WARNING: this can change ownership of files in the project directory")
	Cmd.Flags().IntVarP(&opts.buildNumber, "build-num", "n", 0, "Custom build number (used if can't identify automatically)")
	Cmd.Flags().StringVarP(&opts.buildID, "build-id", "i", "", "Custom build id (used if can't identify automatically)")
	Cmd.Flags().StringVarP(&opts.provider, "provider", "p", "", "Override detected build provider name")
}

func permissionFixDecision(explicit bool) (bool, string) {
	if explicit {
		return true, "requested explicitly with --fix-permissions"
	}

	return false, "--fix-permissions was not set"
}

func applyCIBuildInputFlags(input *types.NewBuildFromCIInput, opts options) error {
	if opts.buildID != "" {
		input.BuildID = opts.buildID
	}
	if input.BuildID == "" {
		return errors.New("build id must be specified")
	}

	if opts.buildNumber != 0 {
		input.BuildNum = opts.buildNumber
	}
	if input.BuildNum == 0 {
		return errors.New("build number must be specified")
	}

	if opts.provider != "" {
		input.Provider = opts.provider
	}
	if input.Provider == "" {
		return errors.New("provider must be specified")
	}

	return nil
}

func findMainServiceBuildConfig(services []*types.AppServiceBuildConfig) (*types.AppServiceBuildConfig, error) {
	for _, service := range services {
		if service.Main {
			return service, nil
		}
	}

	return nil, errors.New("main service not found")
}

func readPostDeployment(context string) (string, error) {
	postDeploymentFile := filepath.Join(context, ".wodby", "post-deployment.yml")
	if _, err := os.Stat(postDeploymentFile); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	content, err := os.ReadFile(postDeploymentFile)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
