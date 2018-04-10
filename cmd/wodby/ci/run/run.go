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
	services   []string
	image      string
	volumes    []string
	env        []string
	user       string
	entrypoint string
	path	   string
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

		var images []string

		if len(opts.services) != 0 {
			for _, svc := range opts.services {
				// Find services by prefix.
				if svc[len(svc)-1] == '-' {
					matchingServices, err := config.FindServicesByPrefix(svc)

					if err != nil {
						return err
					}

					for _, service := range matchingServices {
						fmt.Printf("Found matching svc %s\n", service.Name)
						images = append(images, service.Image)
					}
				} else {
					service, err := config.FindService(svc)

					if err != nil {
						return err
					}

					images = append(images, service.Image)
				}
			}
		} else if opts.image == "" {
			images = append(images, config.Stack.Services[config.Stack.Default].Image)
		} else {
			images = append(images, opts.image)
		}

		if len(images) == 0 {
			return errors.New("No valid images found for this run")
		}

		for _, image := range images {
			runConfig := docker.RunConfig{
				Image:      image,
				Volumes:    opts.volumes,
				Env:        opts.env,
				User:       opts.user,
				Entrypoint: opts.entrypoint,
				Path: 		opts.path,
			}

			return Run(args, runConfig)
		}

		return nil
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
	runConfig.WorkDir = "/mnt/codebase/" + runConfig.Path

	return dockerClient.Run(args, runConfig)
}

func init() {
	Cmd.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "Entrypoint")
	Cmd.Flags().StringSliceVarP(&opts.services, "services", "s", []string{}, "Service")
	Cmd.Flags().StringVarP(&opts.image, "image", "i", "", "Image")
	Cmd.Flags().StringSliceVarP(&opts.volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&opts.env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "User")
	Cmd.Flags().StringVarP(&opts.path, "path", "p", "", "Working dir (relative path)")
}
