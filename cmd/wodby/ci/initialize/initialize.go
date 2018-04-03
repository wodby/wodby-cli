package initialize

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"os/exec"

	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/pborman/uuid"
	"fmt"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
	"gopkg.in/yaml.v2"
	"github.com/wodby/wodby-cli/pkg/types"
	"strings"
)

type commandParams struct {
	UUID    string
	Context string
	DinD    bool
	Volumes []string
	Env     []string
	User    string
}

var params commandParams

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "init [instance UUID]",
	Short: "Initialize config for CI process",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.Errorf("accepts %d arg(s), received %d", 1, len(args))
		}

		return nil
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if viper.GetString("api_key") == "" {
			return errors.New("api-key flag is required")
		}

		params.UUID = args[0]

		var err error
		if params.Context != "" {
			params.Context, err = filepath.Abs(params.Context)
			if err != nil {
				return err
			}
		} else {
			params.Context, err = os.Getwd()
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

		fmt.Print(fmt.Sprintf("Requesting build info for instance %s...", params.UUID))

		stack, err := client.GetBuildConfig(params.UUID)
		if err != nil {
			return err
		}

		fmt.Println(" DONE")

		config := config.Config{
			API:      apiConfig,
			UUID:     params.UUID,
			Context:  params.Context,
			Stack:    stack,
			Metadata: types.NewBuildMetadata(),
		}

		fmt.Println(fmt.Sprintf("Configuring build for instance \"%s\":", config.Stack.Instance.Title))

		dind := false
		if params.DinD {
			dind = true
		} else if config.Metadata.Provider == types.CircleCIName {
			source, err := ioutil.ReadFile(filepath.Join(params.Context, ".circleci/config.yml"))
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
		} else if config.Metadata.Provider == types.CodeshipProCIName {
			dind = true
		}

		if dind {
			fmt.Println("Using docker in docker build schema")

			config.DataContainer = uuid.New()

			_, err := exec.Command("docker", "create", "--volume=/mnt/codebase", fmt.Sprintf("--name=%s", config.DataContainer), config.Stack.Default, "/bin/true").CombinedOutput()
			if err != nil {
				return err
			}

			_, err = exec.Command("docker", "cp", fmt.Sprintf("%s/.", config.Context), fmt.Sprintf("%s:/mnt/codebase", config.DataContainer)).CombinedOutput()
			if err != nil {
				return err
			}
		}

		dockerClient := docker.NewClient()

		if config.Stack.Init != nil {
			for _, service := range config.Stack.Services {
				if service.Name == config.Stack.Init.Service {
					fmt.Println(fmt.Sprintf("Initializing service %s", service.Name))

					user := service.CI.Build.User
					if params.User != "" {
						user = params.User
					}

					runConfig := docker.RunConfig{
						Image:   service.Image,
						Volumes: params.Volumes,
						Env:     params.Env,
						User:    user,
					}

					for envName, envVal := range config.Stack.Init.Environment {
						runConfig.Env = append(runConfig.Env, fmt.Sprintf("%s=%s", envName, envVal))
					}

					if config.DataContainer != "" {
						runConfig.VolumesFrom = []string{config.DataContainer}
						runConfig.WorkDir = "/mnt/codebase"
					} else {
						runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:/mnt/codebase", config.Context))
					}

					err := dockerClient.Run(strings.Split(config.Stack.Init.Command, " "), runConfig)
					if err != nil {
						return err
					}
					break
				}
			}
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(path.Join("/tmp/.wodby-ci.json"), content, 0600)
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&params.Context, "context", "c", "", "Build context (default: current directory)")
	Cmd.Flags().BoolVar(&params.DinD, "dind", false, "Use data container for sharing files between commands")
	Cmd.Flags().StringSliceVarP(&params.Volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&params.Env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&params.User, "user", "u", "", "User")
}
