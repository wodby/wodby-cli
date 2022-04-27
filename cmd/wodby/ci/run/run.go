package run

import (
	"path"

	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
)

type options struct {
	services   []string
	image      string
	volumes    []string
	env        []string
	envFile    string
	user       string
	entrypoint string
	path       string
}

var opts options
var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Run container",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		v.SetConfigFile(path.Join(viper.GetString("ci_config_path")))

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
			images = append(images, config.BuildConfig.Services[config.BuildConfig.Default].Image)
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
				EnvFile:    opts.envFile,
				User:       opts.user,
				Entrypoint: opts.entrypoint,
			}

			dockerClient := docker.NewClient()

			workingDir, err := dockerClient.GetImageWorkingDir(image)

			if err != nil {
				return err
			}

			if config.DataContainer != "" {
				runConfig.VolumesFrom = []string{config.DataContainer}
			} else {
				runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", config.Context, workingDir))
			}

			if opts.path != "" {
				runConfig.WorkDir = fmt.Sprintf("%s/%s", workingDir, opts.path)
			}

			err = dockerClient.Run(args, runConfig)

			if err != nil {
				return err
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "Entrypoint")
	Cmd.Flags().StringSliceVarP(&opts.services, "services", "s", []string{}, "Service")
	Cmd.Flags().StringVarP(&opts.image, "image", "i", "", "Image")
	Cmd.Flags().StringSliceVarP(&opts.volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&opts.env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "User")
	Cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Env file")
	Cmd.Flags().StringVarP(&opts.path, "path", "p", "", "Working dir (relative path)")
}
