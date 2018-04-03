package run

import (
	"os"
	"path"

	"fmt"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/pkg/errors"
)

type commandParams struct {
	Service    string
	Image      string
	Volumes    []string
	Env        []string
	User       string
	Entrypoint string
}

var params commandParams

var ciConfig = viper.New()

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Run container",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		ciConfig.SetConfigFile(path.Join(os.Getenv("HOME"), ".wodby-ci.json"))

		err := ciConfig.ReadInConfig()
		if err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		config := new(config.Config)

		err := ciConfig.Unmarshal(config)
		if err != nil {
			return err
		}

		client := docker.NewClient()

		if params.Service != "" {
			for _, service := range config.Stack.Services {
				if service.Name == params.Service {
					params.Image = service.Image
				}
			}
		} else if params.Image == "" {
			for _, service := range config.Stack.Services {
				if service.Name == config.Stack.Default {
					params.Image = service.Image
				}
			}
		}

		if params.Image == "" {
			return errors.New("image or service must be specified")
		}

		runConfig := docker.RunConfig{
			Image:      params.Image,
			Volumes:    params.Volumes,
			Env:        params.Env,
			User:       params.User,
			Entrypoint: params.Entrypoint,
		}

		if config.DataContainer != "" {
			runConfig.VolumesFrom = []string{config.DataContainer}
		} else {
			runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:/mnt/codebase", config.Context))
		}
		runConfig.WorkDir = "/mnt/codebase"

		err = client.Run(args, runConfig)
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&params.Entrypoint, "entrypoint", "", "Entrypoint")
	Cmd.Flags().StringVarP(&params.Service, "service", "s", "", "Service")
	Cmd.Flags().StringVarP(&params.Image, "image", "i", "", "Image")
	Cmd.Flags().StringSliceVarP(&params.Volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&params.Env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&params.User, "user", "u", "", "User")
}
