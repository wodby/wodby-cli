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
	"github.com/wodby/wodby-cli/cmd/wodby/ci/run"
)

type options struct {
	uuid    	string
	context 	string
	dind    	bool
	skipPermFix bool
}

var opts options

var Cmd = &cobra.Command{
	Use:   "init INSTANCE_UUID",
	Short: "Initialize config for CI process",
	Args: cobra.ExactArgs(1),
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

		fmt.Print(fmt.Sprintf("Requesting build info for instance %s...", opts.uuid))

		stack, err := client.GetBuildConfig(opts.uuid)
		if err != nil {
			return err
		}

		fmt.Println(" DONE")

		config := config.Config{
			API:      apiConfig,
			UUID:     opts.uuid,
			Context:  opts.context,
			Stack:    stack,
			Metadata: types.NewBuildMetadata(),
		}

		fmt.Println(fmt.Sprintf("Preparing build for instance \"%s\"", config.Stack.Instance.Title))
		dind := false

		if opts.dind {
			dind = true
		} else if config.Metadata.Provider == types.CircleCIName {
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
		}

		if dind {
			fmt.Print("Using docker in docker build schema. Creating data container...")

			config.DataContainer = uuid.New()

			_, err := exec.Command("docker", "create", "--volume=/mnt/codebase", fmt.Sprintf("--name=%s", config.DataContainer), config.Stack.Default, "/bin/true").CombinedOutput()
			if err != nil {
				return err
			}

			_, err = exec.Command("docker", "cp", fmt.Sprintf("%s/.", config.Context), fmt.Sprintf("%s:/mnt/codebase", config.DataContainer)).CombinedOutput()
			if err != nil {
				return err
			}

			fmt.Println("DONE")
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(path.Join("/tmp/.wodby-ci.json"), content, 0600)
		if err != nil {
			return err
		}

		// Fixing permissions for managed stacks.
		if !config.Stack.Custom && !opts.skipPermFix {
			dockerClient := docker.NewClient()
			service := config.Stack.Services[config.Stack.Default]
			defaultUser, err := dockerClient.GetDefaultImageUser(service.Image)

			if err != nil {
				return err
			}

			if defaultUser != "root" {
				fmt.Print("Fixing codebase permissions...")

				runConfig := docker.RunConfig{
					Image: service.Image,
					User:  "root",
				}

				args := []string{"chown", "-R", fmt.Sprintf("%s:%s", defaultUser, defaultUser), "."}
				err = run.Run(args, runConfig)

				if err != nil {
					return err
				}

				fmt.Println("DONE")
			}
		}

		// Initializing managed stack services.
		if config.Stack.Init != nil {
			service := config.Stack.Services[config.Stack.Init.Service]

			fmt.Print(fmt.Sprintf("Initializing service %s...", service.Name))

			runConfig := docker.RunConfig{
				Image:   service.Image,
			}

			for envName, envVal := range config.Stack.Init.Environment {
				runConfig.Env = append(runConfig.Env, fmt.Sprintf("%s=%s", envName, envVal))
			}

			err := run.Run(strings.Split(config.Stack.Init.Command, " "), runConfig)

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
	Cmd.Flags().BoolVar(&opts.skipPermFix, "skip-permissions-fix", false, "Skip permissions fix for codebase. May shorten init time")
}
