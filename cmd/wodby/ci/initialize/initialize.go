package initialize

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"fmt"
	"strings"

	"github.com/blang/semver"
	"github.com/pborman/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
	"github.com/wodby/wodby-cli/pkg/version"
	"gopkg.in/yaml.v2"
)

type options struct {
	uuid           string
	context        string
	dind           bool
	buildNumber    string
	username       string
	email          string
	url            string
	fixPermissions bool
	provider       string
	message        string
}

var opts options

var Cmd = &cobra.Command{
	Use:   "init INSTANCE_UUID",
	Short: "Initialize config for CI process",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if viper.GetString("api_key") == "" {
			return errors.New("api-key flag is required")
		}

		opts.uuid = args[0]

		var err error
		if opts.context != "" {
			opts.context, err = filepath.Abs(opts.context)
			if err != nil {
				return err
			}
		} else {
			opts.context, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var logger *log.Logger

		if viper.GetBool("verbose") == true {
			logger = log.New(os.Stdout, "", log.LstdFlags)
		}

		apiConfig := &api.Config{
			Key:    viper.GetString("api_key"),
			Scheme: viper.GetString("api_proto"),
			Host:   viper.GetString("api_host"),
			Prefix: viper.GetString("api_prefix"),
		}
		client := api.NewClient(logger, apiConfig)

		fmt.Println("Checking CLI version...")

		if version.VERSION == "dev" {
			fmt.Println("You're using a dev version of CLI, some things may be unstable. Skipping version check")
		} else {
			ver, err := client.GetLatestVersion()
			if err != nil {
				return err
			}

			v1, err := semver.Make(version.VERSION)
			v2, err := semver.Make(ver)
			if v1.Compare(v2) == -1 {
				return fmt.Errorf("current version of CLI (%s) is outdated, minimum required is %s, please upgrade", v1.String(), v2.String())
			}
		}

		fmt.Printf("Requesting build info for instance %s...", opts.uuid)

		buildConfig, err := client.GetBuildConfig(opts.uuid)
		if err != nil {
			return err
		}

		fmt.Println(" DONE")

		metadata, err := types.NewBuildMetadata(opts.provider, opts.buildNumber, opts.url)

		if err != nil {
			return err
		}

		config := config.Config{
			API:         apiConfig,
			UUID:        opts.uuid,
			Context:     opts.context,
			BuildConfig: buildConfig,
			Metadata:    metadata,
		}

		dind := false

		if opts.dind {
			dind = true
		} else if config.Metadata.Provider == types.CircleCI {
			source, err := ioutil.ReadFile(filepath.Join(opts.context, ".circleci/config.yml"))
			if err != nil {
				return err
			}

			var cfg types.CircleCIConfig
			err = yaml.Unmarshal(source, &cfg)
			if err != nil {
				return err
			}

			if cfg.Jobs.Build.Docker != nil {
				dind = true
			}
		} else if config.Metadata.Provider == types.GitLab {
			dind = true
		}

		dockerClient := docker.NewClient()
		defaultService := config.BuildConfig.Services[config.BuildConfig.Default]
		config.WorkingDir, err = dockerClient.GetImageWorkingDir(defaultService.Image)

		if err != nil {
			return err
		}

		if dind {
			fmt.Print("Using docker in docker build schema. Creating data container...")

			config.DataContainer = uuid.New()

			// We use working dir of the default service for data container.
			output, err := exec.Command("docker", "pull", "alpine").CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}

			output, err = exec.Command("docker", "create", fmt.Sprintf("--volume=%s", config.WorkingDir), fmt.Sprintf("--name=%s", config.DataContainer), "alpine", "/bin/true").CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}

			output, err = exec.Command("docker", "cp", fmt.Sprintf("%s/.", config.Context), fmt.Sprintf("%s:%s", config.DataContainer, config.WorkingDir)).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}

			fmt.Println("DONE")
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(path.Join(viper.GetString("ci_config_path")), content, 0600)
		if err != nil {
			return err
		}

		// We will fix permissions either when it was instructed or when a it's a managed stack and a known CI environment.
		if opts.fixPermissions || (!config.BuildConfig.Custom && metadata.Provider != "Unknown") {
			if opts.fixPermissions {
				fmt.Println("Instructed to fix codebase permissions...")
			} else {
				fmt.Println(fmt.Sprintf("Managed stack detected in a known CI environment %s –  automatically fixing codebase permissions...", metadata.Provider))
			}

			defaultUser, err := dockerClient.GetImageDefaultUser(defaultService.Image)

			if err != nil {
				return err
			}

			if defaultUser != "root" {
				runConfig := docker.RunConfig{
					Image: defaultService.Image,
					User:  "root",
				}

				if config.DataContainer != "" {
					runConfig.VolumesFrom = []string{config.DataContainer}
				} else {
					runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", config.Context, config.WorkingDir))
				}

				args := []string{"chown", "-R", fmt.Sprintf("%s:%s", defaultUser, defaultUser), "."}
				err := dockerClient.Run(args, runConfig)

				if err != nil {
					return err
				}

				fmt.Println("DONE")
			} else {
				fmt.Println("Default user of the default service is root, skipping permissions fix")
			}
		}

		// Initializing managed stack services.
		if config.BuildConfig.Init != nil {
			service := config.BuildConfig.Services[config.BuildConfig.Init.Service]
			workingDir, err := dockerClient.GetImageWorkingDir(service.Image)

			if err != nil {
				return err
			}

			fmt.Printf("Initializing service %s...", service.Name)

			runConfig := docker.RunConfig{
				Image: service.Image,
			}

			for envName, envVal := range config.BuildConfig.Init.Environment {
				runConfig.Env = append(runConfig.Env, fmt.Sprintf("%s=%s", envName, envVal))
			}

			if config.DataContainer != "" {
				runConfig.VolumesFrom = []string{config.DataContainer}
			} else {
				runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", config.Context, workingDir))
			}

			err = dockerClient.Run(strings.Split(config.BuildConfig.Init.Command, " "), runConfig)

			if err != nil {
				return err
			}

			fmt.Println("DONE")
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&opts.context, "context", "c", "", "Build context (default: current directory)")
	Cmd.Flags().BoolVar(&opts.dind, "dind", false, "Use data container for sharing files between commands")
	Cmd.Flags().BoolVar(&opts.fixPermissions, "fix-permissions", false, "Fix codebase permissions. Performed automatically for known CI environments. WARNING: make sure you run wodby ci init from the project directory")
	Cmd.Flags().StringVarP(&opts.buildNumber, "build-num", "n", "", "Custom build number (used if can't identify automatically)")
	Cmd.Flags().StringVar(&opts.url, "url", "", "Custom build url (used if can't acquire automatically)")
	Cmd.Flags().StringVar(&opts.provider, "provider", "p", "Custom build provider name (used if can't identify automatically)")
}
