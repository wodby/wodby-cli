package run

import (
	"path"

	"fmt"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/pkg/errors"
)

type options struct {
	service    string
	image      string
	volumes    []string
	env        []string
	user       string
	entrypoint string
}

var opts options
var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Run container",
	Args: cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		v.SetConfigFile(path.Join( "/tmp/.wodby-ci.json"))

		err := v.ReadInConfig()
		if err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		config := new(config.Config)

		err := v.Unmarshal(config)
		if err != nil {
			return err
		}

		if opts.service != "" {
			for _, service := range config.Stack.Services {
				if service.Name == opts.service {
					opts.image = service.Image
				}
			}
		} else if opts.image == "" {
			for _, service := range config.Stack.Services {
				if service.Name == config.Stack.Default {
					opts.image = service.Image
				}
			}
		}

		if opts.image == "" {
			return errors.New("image or service must be specified")
		}

		runConfig := docker.RunConfig{
			Image:      opts.image,
			Volumes:    opts.volumes,
			Env:        opts.env,
			User:       opts.user,
			Entrypoint: opts.entrypoint,
		}

		return Run(args, runConfig)
	},
}

func Run(args []string, runConfig docker.RunConfig) error {
	cfg := new(config.Config)

	err := v.Unmarshal(cfg)
	if err != nil {
		return err
	}

	dockerClient := docker.NewClient()

	if cfg.DataContainer != "" {
		runConfig.VolumesFrom = []string{cfg.DataContainer}
	} else {
		runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:/mnt/codebase", cfg.Context))
	}
	runConfig.WorkDir = "/mnt/codebase"

	return dockerClient.Run(args, runConfig)
}

func init() {
	Cmd.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "entrypoint")
	Cmd.Flags().StringVarP(&opts.service, "service", "s", "", "service")
	Cmd.Flags().StringVarP(&opts.image, "image", "i", "", "image")
	Cmd.Flags().StringSliceVarP(&opts.volumes, "volume", "v", []string{}, "volumes")
	Cmd.Flags().StringSliceVarP(&opts.env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "user")
}
